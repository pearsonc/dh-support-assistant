package scoring

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// now is the fixed clock every score test reasons against. Using a
// constant keeps the age/due arithmetic deterministic across CI and
// laptop runs — flaky time-based tests are the #1 source of false
// positives in this kind of code.
var now = time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

const floatEpsilon = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < floatEpsilon
}

// TestScoreSeverityBuckets exercises every known severity at age=0 and
// due in DueHorizon (so age_score = 0 and due_score = 0). The result
// should equal w.Severity * sev_score for the default weights.
// Numeric codes and display strings must resolve to the same score — the
// 2.2 integration against the populated DB confirmed this org emits
// numeric Severity; the plan's original display-string spec is kept as
// the authoritative reference form.
func TestScoreSeverityBuckets(t *testing.T) {
	w := DefaultWeights()
	dueFar := now.Add(DueHorizon) // due_score = 0
	tests := []struct {
		severity string
		want     float64
	}{
		// Display strings
		{"Critical", 0.5 * 1.0},
		{"High", 0.5 * 0.75},
		{"Moderate", 0.5 * 0.5},
		{"Medium", 0.5 * 0.5},
		{"Low", 0.5 * 0.25},
		{"Planning", 0.5 * 0.0},
		// Numeric codes
		{"1", 0.5 * 1.0},
		{"2", 0.5 * 0.75},
		{"3", 0.5 * 0.5},
		{"4", 0.5 * 0.25},
		{"5", 0.5 * 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.severity, func(t *testing.T) {
			got := Score(Ticket{Severity: tc.severity, OpenedAt: now, DueDate: dueFar}, w, now)
			if !approxEqual(got, tc.want) {
				t.Errorf("Score(%q) = %v, want %v", tc.severity, got, tc.want)
			}
		})
	}
}

// TestScoreUnknownSeverity: anything outside the mapped set falls to 0.
// Score must still compute age and due contributions.
func TestScoreUnknownSeverity(t *testing.T) {
	w := DefaultWeights()
	got := Score(Ticket{
		Severity: "Catastrophic",
		OpenedAt: now.Add(-AgeHorizon), // age_score = 1
		DueDate:  now.Add(DueHorizon),  // due_score = 0
	}, w, now)
	want := w.Age * 1.0
	if !approxEqual(got, want) {
		t.Errorf("unknown severity score = %v, want %v (age contribution only)", got, want)
	}
}

// TestScoreAgeHorizons covers: fresh (age=0), mid-horizon (age=7d),
// at-horizon (age=14d), past-horizon (age=30d), future-opened (negative age).
func TestScoreAgeHorizons(t *testing.T) {
	w := Weights{Severity: 0, Age: 1, Due: 0} // isolate age
	due := now.Add(DueHorizon)                // zero due contribution

	tests := []struct {
		name    string
		openAge time.Duration
		want    float64
	}{
		{"fresh", 0, 0},
		{"mid-horizon", 7 * 24 * time.Hour, 0.5},
		{"at-horizon", AgeHorizon, 1.0},
		{"past-horizon-clamped", 30 * 24 * time.Hour, 1.0},
		{"future-opened", -24 * time.Hour, 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(Ticket{
				Severity: "Critical",
				OpenedAt: now.Add(-tc.openAge),
				DueDate:  due,
			}, w, now)
			if !approxEqual(got, tc.want) {
				t.Errorf("age=%v -> %v, want %v", tc.openAge, got, tc.want)
			}
		})
	}
}

// TestScoreDueHorizons covers: overdue (due in the past), due-now,
// due-half-horizon, due-at-horizon, due-past-horizon (clamped to 0).
func TestScoreDueHorizons(t *testing.T) {
	w := Weights{Severity: 0, Age: 0, Due: 1} // isolate due

	tests := []struct {
		name      string
		dueOffset time.Duration // relative to now; negative = overdue
		want      float64
	}{
		{"overdue-3d", -3 * 24 * time.Hour, 1.0},
		{"due-now", 0, 1.0},
		{"due-half-horizon", DueHorizon / 2, 0.5},
		{"due-at-horizon", DueHorizon, 0.0},
		{"due-past-horizon-clamped", 14 * 24 * time.Hour, 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(Ticket{
				Severity: "Planning", // sev_score = 0 so it doesn't leak
				OpenedAt: now,
				DueDate:  now.Add(tc.dueOffset),
			}, w, now)
			if !approxEqual(got, tc.want) {
				t.Errorf("dueOffset=%v -> %v, want %v", tc.dueOffset, got, tc.want)
			}
		})
	}
}

// TestScoreCombinations verifies the weighted sum at a few realistic
// intersections. Default weights sum to 1.0 so values stay in [0, 1].
func TestScoreCombinations(t *testing.T) {
	w := DefaultWeights()
	tests := []struct {
		name   string
		ticket Ticket
		want   float64
	}{
		{
			// Critical, 7d old, due in 3.5d.
			// sev=1, age=0.5, due=1-(3.5/7)=0.5 => 0.5*1 + 0.2*0.5 + 0.3*0.5 = 0.75
			name: "balanced-critical",
			ticket: Ticket{
				Severity: "Critical",
				OpenedAt: now.Add(-7 * 24 * time.Hour),
				DueDate:  now.Add(84 * time.Hour),
			},
			want: 0.75,
		},
		{
			// Moderate, fresh, due in 2 weeks (past horizon) => sev only.
			// 0.5*0.5 + 0.2*0 + 0.3*0 = 0.25
			name: "moderate-fresh-far-due",
			ticket: Ticket{
				Severity: "Moderate",
				OpenedAt: now,
				DueDate:  now.Add(14 * 24 * time.Hour),
			},
			want: 0.25,
		},
		{
			// Low, 1 month old, overdue a week => age/due max, sev quarter.
			// 0.5*0.25 + 0.2*1 + 0.3*1 = 0.625
			name: "low-very-old-overdue",
			ticket: Ticket{
				Severity: "Low",
				OpenedAt: now.Add(-30 * 24 * time.Hour),
				DueDate:  now.Add(-7 * 24 * time.Hour),
			},
			want: 0.625,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(tc.ticket, w, now)
			if !approxEqual(got, tc.want) {
				t.Errorf("Score = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestScoreWeightBoundaries verifies weights that don't sum to 1 produce
// a proportionally scaled result (documented behaviour).
func TestScoreWeightBoundaries(t *testing.T) {
	// Weights sum to 2 → max score 2 when every component is 1.
	w := Weights{Severity: 1, Age: 1, Due: 0}
	got := Score(Ticket{
		Severity: "Critical",
		OpenedAt: now.Add(-AgeHorizon),
		DueDate:  now.Add(DueHorizon),
	}, w, now)
	want := 1.0*1.0 + 1.0*1.0 // sev 1 + age 1
	if !approxEqual(got, want) {
		t.Errorf("unnormalised weights: got %v, want %v", got, want)
	}
}

// TestBucketFor checks every threshold including boundary values and an
// out-of-range input at both ends.
func TestBucketFor(t *testing.T) {
	tests := []struct {
		score float64
		want  Bucket
	}{
		{1.0, BucketCritical},
		{0.9, BucketCritical},
		{0.8999, BucketHigh},
		{0.7, BucketHigh},
		{0.699, BucketModerate},
		{0.4, BucketModerate},
		{0.399, BucketLow},
		{0.15, BucketLow},
		{0.149, BucketPlanning},
		{0.0, BucketPlanning},
		{-0.5, BucketPlanning}, // out-of-range clamp via fallthrough
		{2.0, BucketCritical},  // above 1.0 also critical
	}
	for _, tc := range tests {
		if got := BucketFor(tc.score); got != tc.want {
			t.Errorf("BucketFor(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

// TestIsKnownSeverity covers both membership directions.
func TestIsKnownSeverity(t *testing.T) {
	for _, k := range []string{"Critical", "High", "Moderate", "Low", "Planning"} {
		if !IsKnownSeverity(k) {
			t.Errorf("IsKnownSeverity(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"", "critical", "Catastrophic", "Unknown"} {
		if IsKnownSeverity(k) {
			t.Errorf("IsKnownSeverity(%q) = true, want false", k)
		}
	}
}

// TestStaleAsOf exercises every branch of the heuristic.
func TestStaleAsOf(t *testing.T) {
	caller := "alice"
	threshold := 3 * 24 * time.Hour

	recent := now.Add(-24 * time.Hour)  // within 3d threshold
	old := now.Add(-5 * 24 * time.Hour) // older than 3d threshold
	veryOld := now.Add(-10 * 24 * time.Hour)

	tests := []struct {
		name   string
		events []Event
		want   bool
	}{
		{"no events is stale", nil, true},
		{"only caller comments is stale", []Event{
			{EventTS: recent, AuthorName: caller, AuthorContext: "Additional comments"},
		}, true},
		{"work notes by caller counts as support", []Event{
			{EventTS: recent, AuthorName: caller, AuthorContext: "Work Notes"},
		}, false},
		{"support reply within threshold not stale", []Event{
			{EventTS: recent, AuthorName: "bob", AuthorContext: "Additional comments"},
		}, false},
		{"support reply past threshold is stale", []Event{
			{EventTS: old, AuthorName: "bob", AuthorContext: "Additional comments"},
		}, true},
		{"latest of mixed wins (recent caller, old support)", []Event{
			{EventTS: recent, AuthorName: caller, AuthorContext: "Additional comments"},
			{EventTS: old, AuthorName: "bob", AuthorContext: "Additional comments"},
		}, true},
		{"latest of mixed wins (old caller, recent support)", []Event{
			{EventTS: veryOld, AuthorName: caller, AuthorContext: "Additional comments"},
			{EventTS: recent, AuthorName: "bob", AuthorContext: "Additional comments"},
		}, false},
		{"unordered events picks latest support", []Event{
			{EventTS: old, AuthorName: "bob", AuthorContext: "Additional comments"},
			{EventTS: veryOld, AuthorName: "bob", AuthorContext: "Additional comments"},
			{EventTS: recent, AuthorName: "bob", AuthorContext: "Additional comments"},
		}, false},
		{"exactly at threshold not stale (boundary = 3d exact)", []Event{
			{EventTS: now.Add(-threshold), AuthorName: "bob", AuthorContext: "Additional comments"},
		}, false},
		{"just past threshold is stale", []Event{
			{EventTS: now.Add(-threshold - time.Second), AuthorName: "bob", AuthorContext: "Additional comments"},
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StaleAsOf(tc.events, caller, threshold, now)
			if got != tc.want {
				t.Errorf("StaleAsOf = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsSupportSide covers the predicate exposed for reuse by queries.
func TestIsSupportSide(t *testing.T) {
	tests := []struct {
		name   string
		event  Event
		caller string
		want   bool
	}{
		{"work notes any author", Event{AuthorContext: "Work Notes", AuthorName: "alice"}, "alice", true},
		{"work notes from caller still support", Event{AuthorContext: "Work Notes", AuthorName: "alice"}, "alice", true},
		{"additional comments from caller not support", Event{AuthorContext: "Additional comments", AuthorName: "alice"}, "alice", false},
		{"additional comments from other is support", Event{AuthorContext: "Additional comments", AuthorName: "bob"}, "alice", true},
		{"empty author treated as non-caller", Event{AuthorContext: "Additional comments", AuthorName: ""}, "alice", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSupportSide(tc.event, tc.caller); got != tc.want {
				t.Errorf("IsSupportSide = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestScorerWarnOnce verifies the Scorer logs exactly one Warn line per
// distinct unknown Severity string — re-scoring the same unknown value
// a hundred times must NOT flood the log. Second unknown value produces
// a second Warn. Known values never warn.
func TestScorerWarnOnce(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	s := NewScorer(logger, DefaultWeights())

	// Score one known ticket — must not warn.
	_ = s.Score(Ticket{Severity: "High", OpenedAt: now, DueDate: now.Add(DueHorizon)}, now)

	// Score the same unknown severity 50 times — must emit exactly one Warn.
	for i := 0; i < 50; i++ {
		_ = s.Score(Ticket{Severity: "Catastrophic", OpenedAt: now, DueDate: now.Add(DueHorizon)}, now)
	}

	// Different unknown severity — another warn.
	_ = s.Score(Ticket{Severity: "Trivial", OpenedAt: now, DueDate: now.Add(DueHorizon)}, now)

	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 warn lines, got %d: %s", len(lines), buf.String())
	}
	seen := map[string]bool{}
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("unmarshal warn line: %v (%q)", err, string(line))
		}
		if entry["level"] != "warn" {
			t.Errorf("line level = %v, want warn", entry["level"])
		}
		sev, _ := entry["severity"].(string)
		seen[sev] = true
	}
	if !seen["Catastrophic"] || !seen["Trivial"] {
		t.Errorf("missing warn for one of the unknowns; seen=%v", seen)
	}
}

// TestScorerWeightsAccessor round-trips the weights through the scorer
// so /api/stats transparency has a source it can pull from without
// re-reading config.
func TestScorerWeightsAccessor(t *testing.T) {
	w := Weights{Severity: 0.6, Age: 0.1, Due: 0.3}
	s := NewScorer(zerolog.Nop(), w)
	if got := s.Weights(); got != w {
		t.Errorf("Weights = %+v, want %+v", got, w)
	}
}
