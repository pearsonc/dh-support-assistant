package scoring

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// BenchmarkScore_412Tickets measures the cost of scoring a population the
// size of the Phase 0 sample export (412 tickets). The plan budgets <5ms
// total; this benchmark lets us regress-guard when weight formulas change.
// Synthetic inputs rotate through the five known severities so the branch
// predictor doesn't get a free ride on a single-severity micro-bench.
func BenchmarkScore_412Tickets(b *testing.B) {
	nowT := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	tickets := make([]Ticket, 412)
	sevs := []string{"Critical", "High", "Moderate", "Low", "Planning"}
	for i := range tickets {
		tickets[i] = Ticket{
			Severity: sevs[i%len(sevs)],
			OpenedAt: nowT.Add(-time.Duration(i) * time.Hour),
			DueDate:  nowT.Add(time.Duration(i) * time.Hour),
		}
	}
	w := DefaultWeights()

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var total float64
		for _, t := range tickets {
			total += Score(t, w, nowT)
		}
		_ = total
	}
}

// BenchmarkScorer_412Tickets is the Scorer-wrapped variant including the
// warn-once sync.Map lookup path. Equal or marginally slower than the
// pure benchmark; the divergence quantifies the Scorer overhead when
// scoring lists at list-endpoint request time.
func BenchmarkScorer_412Tickets(b *testing.B) {
	nowT := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	tickets := make([]Ticket, 412)
	sevs := []string{"Critical", "High", "Moderate", "Low", "Planning"}
	for i := range tickets {
		tickets[i] = Ticket{
			Severity: sevs[i%len(sevs)],
			OpenedAt: nowT.Add(-time.Duration(i) * time.Hour),
			DueDate:  nowT.Add(time.Duration(i) * time.Hour),
		}
	}
	s := NewScorer(zerolog.Nop(), DefaultWeights())

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var total float64
		for _, t := range tickets {
			total += s.Score(t, nowT)
		}
		_ = total
	}
}

// BenchmarkStaleAsOf_50Events bounds the staleness computation for a
// ticket with a typical event-count (Phase 0 sample averaged ~7 events
// per ticket; 50 is a long-running ticket's upper bound).
func BenchmarkStaleAsOf_50Events(b *testing.B) {
	nowT := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	events := make([]Event, 50)
	for i := range events {
		events[i] = Event{
			EventTS:       nowT.Add(-time.Duration(i) * time.Hour),
			AuthorName:    "bob",
			AuthorContext: "Additional comments",
		}
	}
	threshold := 3 * 24 * time.Hour
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = StaleAsOf(events, "alice", threshold, nowT)
	}
}
