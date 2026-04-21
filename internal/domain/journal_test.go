package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseJournalEmpty(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "   ", "\t\n\r\n"} {
		entries, err := ParseJournal(raw)
		if err != nil {
			t.Errorf("ParseJournal(%q) unexpected err: %v", raw, err)
		}
		if entries != nil {
			t.Errorf("ParseJournal(%q) expected nil slice, got %d entries", raw, len(entries))
		}
	}
}

func TestParseJournalSingleEntry(t *testing.T) {
	t.Parallel()
	cell := "2026-04-10 09:14:22 - Chris Pearson (Service Desk)\n Please can you confirm the change window."
	entries, err := ParseJournal(cell)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.AuthorName != "Chris Pearson" {
		t.Errorf("author: got %q", e.AuthorName)
	}
	if e.AuthorContext != "Service Desk" {
		t.Errorf("context: got %q", e.AuthorContext)
	}
	want, _ := time.Parse("2006-01-02 15:04:05", "2026-04-10 09:14:22")
	if !e.EventTS.Equal(want) {
		t.Errorf("ts: got %v want %v", e.EventTS, want)
	}
	if e.Body != " Please can you confirm the change window." {
		t.Errorf("body preserves leading space: got %q", e.Body)
	}
}

func TestParseJournalSortsAscendingFromNewestFirstSource(t *testing.T) {
	t.Parallel()
	// ServiceNow renders newest-first per Phase 0. We assert the parser
	// output is ascending regardless so the writer inserts chronologically.
	cell := strings.Join([]string{
		"2026-04-12 10:00:00 - Alice (Support)",
		" Third in time",
		"",
		"2026-04-11 10:00:00 - Bob (Support)",
		" Second in time",
		"",
		"2026-04-10 10:00:00 - Carol (Support)",
		" First in time",
	}, "\n")

	entries, err := ParseJournal(cell)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	wantAuthors := []string{"Carol", "Bob", "Alice"}
	for i, want := range wantAuthors {
		if entries[i].AuthorName != want {
			t.Errorf("entries[%d].author: got %q want %q", i, entries[i].AuthorName, want)
		}
	}
	for i := 1; i < len(entries); i++ {
		if !entries[i-1].EventTS.Before(entries[i].EventTS) {
			t.Errorf("entries not ascending at index %d", i)
		}
	}
}

func TestParseJournalHeaderWithNestedParentheses(t *testing.T) {
	t.Parallel()
	// Phase 0 Comment History Approach: Author Context may carry nested
	// parens like "Service Desk (UK)". The non-greedy regex must capture
	// the outermost pair.
	cell := "2026-04-10 09:14:22 - Chris Pearson (Service Desk (UK))\n Body line."
	entries, err := ParseJournal(cell)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].AuthorContext != "Service Desk (UK)" {
		t.Errorf("nested-paren context: got %q", entries[0].AuthorContext)
	}
}

func TestParseJournalMalformed(t *testing.T) {
	t.Parallel()
	// Content but no header: the cell is populated noise without any
	// parseable entry. The schema requires author + timestamp per row
	// (author_context NOT NULL) so we refuse the whole cell rather than
	// silently drop it.
	cell := "Some free text with no timestamp header at all."
	_, err := ParseJournal(cell)
	if !errors.Is(err, ErrMalformedJournal) {
		t.Errorf("got err=%v, want ErrMalformedJournal", err)
	}
}

func TestParseJournalCRLFLines(t *testing.T) {
	t.Parallel()
	// CRLF-sourced cells must still parse. internal/ingest.Normalise
	// strips CR at file level, but defence-in-depth here keeps the parser
	// usable against ad-hoc string inputs in future unit tests.
	cell := "2026-04-10 09:14:22 - Alice (Support)\r\n Body\r\n\r\n2026-04-09 08:00:00 - Bob (Support)\r\n Earlier body"
	entries, err := ParseJournal(cell)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].AuthorName != "Bob" || entries[1].AuthorName != "Alice" {
		t.Errorf("ordering wrong: %q %q", entries[0].AuthorName, entries[1].AuthorName)
	}
}

func TestParseJournal14KBCellWithSevenEntries(t *testing.T) {
	t.Parallel()
	// Plan hard-stop: "given a 14 KB cell with 7 entries, all entries
	// extracted; order is ascending by event_ts in the database
	// (regardless of newest-first source order)."
	//
	// We synthesise 7 entries, each with ~2 KB body, emitted newest-first
	// (as ServiceNow does). Total cell size must exceed 14 KB so the
	// 14,364-char max Phase 0 observed is exercised at the same order of
	// magnitude.
	const entryCount = 7
	const bodyBytes = 2048
	filler := strings.Repeat("x", bodyBytes)

	var sb strings.Builder
	// Emit newest first: i=entryCount-1 → i=0 (earliest).
	for i := entryCount - 1; i >= 0; i-- {
		// Minute-apart timestamps so ordering is deterministic.
		hdr := fmt.Sprintf("2026-04-10 09:%02d:00 - Author%d (Service Desk)\n", i, i)
		sb.WriteString(hdr)
		sb.WriteString(" ")
		sb.WriteString(filler)
		sb.WriteString("\n")
		if i > 0 {
			sb.WriteString("\n")
		}
	}
	cell := sb.String()
	if len(cell) < 14*1024 {
		t.Fatalf("fixture too small: %d bytes; need ≥ 14 KB to match Phase 0 observation", len(cell))
	}

	entries, err := ParseJournal(cell)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(entries) != entryCount {
		t.Fatalf("want %d entries, got %d", entryCount, len(entries))
	}

	for i, e := range entries {
		wantName := fmt.Sprintf("Author%d", i)
		if e.AuthorName != wantName {
			t.Errorf("entries[%d].author: got %q want %q", i, e.AuthorName, wantName)
		}
		if e.AuthorContext != "Service Desk" {
			t.Errorf("entries[%d].context: got %q", i, e.AuthorContext)
		}
		if len(e.Body) < bodyBytes {
			t.Errorf("entries[%d].body too short: %d bytes", i, len(e.Body))
		}
	}
	for i := 1; i < len(entries); i++ {
		if !entries[i-1].EventTS.Before(entries[i].EventTS) {
			t.Errorf("entries[%d] not strictly after entries[%d]: %v vs %v",
				i, i-1, entries[i].EventTS, entries[i-1].EventTS)
		}
	}
}
