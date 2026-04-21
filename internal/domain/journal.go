package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// journalHeaderRE matches one entry header in the merged "Comments and Work
// notes" cell. The anchors and groups come from Phase 0 Comment History
// Approach A:
//
//	^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) - (.+?) \((.+?)\)\s*$
//
// Groups: 1=timestamp, 2=author name, 3=author context. Non-greedy `.+?`
// against the trailing anchor lets nested parentheses inside the context
// (e.g. "Service Desk (UK)") still capture the outermost pair — RE2 expands
// the capture until `\)\s*$` succeeds at the very end of the line.
var journalHeaderRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) - (.+?) \((.+?)\)\s*$`)

const journalHeaderLayout = "2006-01-02 15:04:05"

// JournalEntry is one parsed entry from a merged Comments and Work notes
// cell. Type (comment vs work note) is NOT recoverable from the merged
// column per Phase 0 — Phase 6 Approach B handles that separately.
type JournalEntry struct {
	EventTS       time.Time
	AuthorName    string
	AuthorContext string
	Body          string
}

// ErrMalformedJournal is returned when the cell has non-whitespace content
// but no parseable entry header. Empty / whitespace-only input returns
// (nil, nil) since Phase 0 observed 96.4% journal population — the 3.6% of
// tickets with no comments are a normal case, not an error.
var ErrMalformedJournal = errors.New("domain: journal cell had content but no parseable entry header")

// ParseJournal walks a merged Comments and Work notes cell in source order
// (Phase 0 observed newest-first) and returns entries sorted ASCENDING by
// EventTS. Sorting happens here so the writer's insertion order matches
// chronological order regardless of how ServiceNow rendered the cell.
//
// Timestamps are interpreted as UTC per the Phase 1 plan pre-flight
// decision ("parse as UTC on ingest; reconsider if timezone drift is
// observed on re-export comparison").
func ParseJournal(cell string) ([]JournalEntry, error) {
	if strings.TrimSpace(cell) == "" {
		return nil, nil
	}

	lines := strings.Split(cell, "\n")
	var entries []JournalEntry
	var current *JournalEntry
	var body strings.Builder

	flush := func() {
		if current == nil {
			return
		}
		current.Body = strings.TrimRight(body.String(), "\n")
		entries = append(entries, *current)
		current = nil
		body.Reset()
	}

	for _, line := range lines {
		// Strip a trailing CR so CRLF-sourced cells still match the
		// anchored header regex. internal/ingest.Normalise also folds
		// CRLF → LF at file level; this defence keeps the parser usable
		// standalone (e.g. inside unit tests on raw literal cells).
		line = strings.TrimRight(line, "\r")
		if m := journalHeaderRE.FindStringSubmatch(line); m != nil {
			flush()
			ts, err := time.ParseInLocation(journalHeaderLayout, m[1], time.UTC)
			if err != nil {
				return nil, fmt.Errorf("domain: parse journal header timestamp %q: %w", m[1], err)
			}
			current = &JournalEntry{
				EventTS:       ts,
				AuthorName:    m[2],
				AuthorContext: m[3],
			}
			continue
		}
		if current == nil {
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	flush()

	if len(entries) == 0 {
		return nil, ErrMalformedJournal
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].EventTS.Before(entries[j].EventTS)
	})
	return entries, nil
}
