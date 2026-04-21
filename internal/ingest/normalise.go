// Package ingest implements the xlsx → Postgres pipeline. Sub-packages
// don't exist — all files in this directory co-operate as one unit:
// normalise, reader, mapper, writer, and the orchestrator (ingest.go).
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// enDash is U+2013 (EN DASH). Phase 0 observed that the "Assignment Group"
// export column uses an en-dash in one of seven sub-groups while the
// remaining six use an ASCII hyphen (U+002D). Phase 1 plan pre-flight
// mandates folding en-dash → hyphen in internal/ingest so grouping,
// dedup, and client comparison work without visual-but-invisible
// mismatches.
const (
	enDash     = "–"
	asciiHypen = "-"
)

// NormaliseCell returns s with en-dashes folded to hyphens, trailing CR
// stripped, and trailing whitespace trimmed. Leading whitespace is
// preserved because journal entry bodies legitimately begin with a leading
// space (Phase 0 Comment History finding: "body follows on the next
// line(s) often with a leading space").
//
// Called at two points in the pipeline:
//  1. Per-cell during xlsx read so downstream parsers and the SHA-256
//     import hash see a canonical representation.
//  2. Defensively on Assignment Group in the mapper in case a future
//     reader path bypasses cell normalisation.
func NormaliseCell(s string) string {
	s = strings.ReplaceAll(s, enDash, asciiHypen)
	s = strings.TrimRight(s, " \t\r\n")
	return s
}

// ContentHash returns the hex SHA-256 of the normalised logical content of
// an export. Header row (index 0) plus all data rows are tab-joined per
// row and newline-joined across rows; each cell is passed through
// NormaliseCell first. Two invocations of `make ingest FILE=X` on the
// same file therefore produce the same hash so the imports.import_hash
// UNIQUE constraint can short-circuit the second run per [Rule: Ingest
// idempotency].
//
// Hashing logical content (not raw xlsx bytes) sidesteps the xlsx metadata
// timestamps that would otherwise make every re-export look different
// even when the underlying rows are identical.
func ContentHash(rows [][]string) string {
	h := sha256.New()
	for i, row := range rows {
		if i > 0 {
			h.Write([]byte{'\n'})
		}
		for j, cell := range row {
			if j > 0 {
				h.Write([]byte{'\t'})
			}
			h.Write([]byte(NormaliseCell(cell)))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
