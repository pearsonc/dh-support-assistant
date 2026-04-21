// Package scoring computes the priority score and staleness flag surfaced
// on every ticket in the Phase 2 dashboard. The formula is defined in
// Plan 2026-04-21-phase-2-dashboard-api §Decision 3 (scoring) and §Decision
// 4 (staleness); this package is the only production owner of either.
//
// The core `Score` and `StaleAsOf` functions are pure — they take their
// inputs explicitly and return a value with no side effects. The `Scorer`
// wrapper pairs `Score` with a zerolog sink that warns once per distinct
// unknown Severity string so large batches of mis-tagged tickets don't
// spam the log.
package scoring

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Weights controls the relative contribution of severity, age, and due-date
// proximity to the final score. Weights are NOT normalised — with the
// defaults (0.5 + 0.2 + 0.3 = 1.0) the score range is [0, 1]; callers that
// override the weights take responsibility for the resulting range.
type Weights struct {
	Severity float64
	Age      float64
	Due      float64
}

// DefaultWeights mirrors cfg.Scoring defaults used by cmd/server. Kept here
// so scoring tests don't have to import config.
func DefaultWeights() Weights {
	return Weights{Severity: 0.5, Age: 0.2, Due: 0.3}
}

// Ticket is the minimal projection required to score a row. Both queries
// and scoring tests construct this directly; it intentionally does NOT
// import internal/queries to avoid cycles.
type Ticket struct {
	Severity string
	OpenedAt time.Time
	DueDate  time.Time
}

// Event is the minimal projection required to evaluate staleness. Mirrors
// the three fields from queries.TicketEventItem that feed the heuristic.
type Event struct {
	EventTS       time.Time
	AuthorName    string
	AuthorContext string
}

// Horizons for the age and due components. Fixed in Phase 2 — the
// tuneables are the weights, not the horizons. Lifting these to config
// is an open question for a future phase if the defaults mis-score.
const (
	AgeHorizon = 14 * 24 * time.Hour
	DueHorizon = 7 * 24 * time.Hour
)

// AuthorContextWorkNotes matches ServiceNow's "Work notes" merged-journal
// label (the 0005 migration spec). Work notes are always internal, so
// their presence never suggests client-side silence.
const AuthorContextWorkNotes = "Work Notes"

// severityScore is the authoritative ServiceNow Severity → score mapping.
// Both forms appear in the wild: Plan §Decision-3 wrote the mapping using
// the display labels (Critical / High / Moderate / Low / Planning) but the
// Phase 2.2 integration against the populated DB showed that this org's
// ServiceNow export emits Severity as numeric codes (1–4) — while Priority
// is the column carrying display strings. "Medium" is the org's local
// variant of the ServiceNow-default "Moderate". Mapping both forms keeps
// scoring correct regardless of which column the caller passes in, and
// future-proofs against a ServiceNow re-configuration.
var severityScore = map[string]float64{
	// Display strings (plan Decision 3 original spec)
	"Critical": 1.0,
	"High":     0.75,
	"Moderate": 0.5,
	"Medium":   0.5,
	"Low":      0.25,
	"Planning": 0.0,
	// ServiceNow numeric severity codes (observed in this org's export)
	"1": 1.0,
	"2": 0.75,
	"3": 0.5,
	"4": 0.25,
	"5": 0.0,
}

// Bucket collapses a score into one of five stable strings for the
// dashboard's priority-distribution stats block. The thresholds are
// conservative (a maximally-stale Low ticket tops out at 0.75) so the
// high-urgency buckets stay small.
type Bucket string

const (
	BucketCritical Bucket = "critical"
	BucketHigh     Bucket = "high"
	BucketModerate Bucket = "moderate"
	BucketLow      Bucket = "low"
	BucketPlanning Bucket = "planning"
)

// BucketFor maps a score in [0, 1] to a Bucket. Anything outside that
// range is clamped into the nearest terminal bucket so a pathological
// custom weighting can't produce an empty bucket label.
func BucketFor(score float64) Bucket {
	switch {
	case score >= 0.9:
		return BucketCritical
	case score >= 0.7:
		return BucketHigh
	case score >= 0.4:
		return BucketModerate
	case score >= 0.15:
		return BucketLow
	default:
		return BucketPlanning
	}
}

// IsKnownSeverity reports whether sev is one of the mapped ServiceNow
// Severity values. Exposed so callers (specifically Scorer) can decide
// whether to fire a warn-once log entry without re-running the score.
func IsKnownSeverity(sev string) bool {
	_, ok := severityScore[sev]
	return ok
}

// Score computes a priority score in [0, 1] for the given ticket.
//
//	score = w.Severity * sev_score
//	      + w.Age      * age_score
//	      + w.Due      * due_score
//
// sev_score is 0 for any Severity outside the mapped set. age_score is
// min(1, (now - opened_at) / AgeHorizon) — tickets opened in the future
// score 0. due_score is clamp(0, 1, 1 - (due - now) / DueHorizon) — an
// overdue ticket scores 1; a ticket due in ≥ DueHorizon scores 0.
//
// Pure: no logging, no globals mutated. Caller is responsible for the
// warn-once on unknown severities (see Scorer).
func Score(t Ticket, w Weights, now time.Time) float64 {
	sev := severityScore[t.Severity] // zero when not present; matches plan spec

	age := now.Sub(t.OpenedAt)
	ageScore := 0.0
	if age > 0 {
		ageScore = float64(age) / float64(AgeHorizon)
		if ageScore > 1 {
			ageScore = 1
		}
	}

	dueScore := 1 - float64(t.DueDate.Sub(now))/float64(DueHorizon)
	if dueScore < 0 {
		dueScore = 0
	} else if dueScore > 1 {
		dueScore = 1
	}

	return w.Severity*sev + w.Age*ageScore + w.Due*dueScore
}

// StaleAsOf reports whether a ticket with the given event history and
// caller should be flagged stale as of `now`. "Support-side" per plan
// Decision 4: an event is support-side iff AuthorContext == "Work Notes"
// OR AuthorName != caller. The result is `true` when no support-side
// event has landed within `threshold`. A ticket with no events at all is
// stale by definition — the caller has spoken (the ticket opening) but
// support has not yet replied.
//
// Events are not assumed to be sorted; StaleAsOf scans the slice once
// and tracks the latest support-side timestamp. The caller of origin
// (`ticket.Caller`) is what distinguishes their messages from the rest.
func StaleAsOf(events []Event, caller string, threshold time.Duration, now time.Time) bool {
	var latest time.Time
	found := false
	for _, e := range events {
		if !isSupportSide(e, caller) {
			continue
		}
		if !found || e.EventTS.After(latest) {
			latest = e.EventTS
			found = true
		}
	}
	if !found {
		return true
	}
	return now.Sub(latest) > threshold
}

// IsSupportSide returns true when e came from support rather than the
// ticket's caller. Exposed so the queries layer can reuse the same
// predicate when it needs the most-recent support timestamp without
// calling StaleAsOf twice.
func IsSupportSide(e Event, caller string) bool {
	return isSupportSide(e, caller)
}

func isSupportSide(e Event, caller string) bool {
	if e.AuthorContext == AuthorContextWorkNotes {
		return true
	}
	return e.AuthorName != caller
}

// Scorer wraps Score with a concurrent-safe warn-once sink. One Scorer
// per server boot; a given unknown Severity string produces exactly one
// Warn line for that process lifetime, preventing log spam when a large
// ingest lands a new mis-tagged value across many tickets.
type Scorer struct {
	weights Weights
	logger  zerolog.Logger
	seen    sync.Map // map[string]struct{}
}

// NewScorer binds a logger and the weights the server runs with. The
// logger may be zerolog.Nop() for tests that don't care about warnings.
func NewScorer(logger zerolog.Logger, weights Weights) *Scorer {
	return &Scorer{weights: weights, logger: logger}
}

// Weights returns the configured weights. Exposed mostly for UI
// transparency on /api/stats and for log lines that want to record
// which weighting produced a given ordering.
func (s *Scorer) Weights() Weights { return s.weights }

// Score evaluates a ticket and emits a single Warn line the first time
// a given unknown severity is seen by this Scorer.
func (s *Scorer) Score(t Ticket, now time.Time) float64 {
	if !IsKnownSeverity(t.Severity) {
		if _, loaded := s.seen.LoadOrStore(t.Severity, struct{}{}); !loaded {
			s.logger.Warn().
				Str("severity", t.Severity).
				Msg("scoring: unknown severity — treating as 0")
		}
	}
	return Score(t, s.weights, now)
}
