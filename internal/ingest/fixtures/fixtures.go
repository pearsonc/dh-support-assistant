// Package fixtures builds synthetic ServiceNow xlsx exports for the ingest
// test suite. Every byte of content is invented. Under no circumstances
// may a fixture file be copied or derived from a real ServiceNow export
// — test data is governed by the same [Rule: Zero Egress] that governs
// runtime behaviour, and a real-data leak into `internal/ingest/fixtures`
// would persist in git history even after removal.
//
// Fixtures are written to the caller's t.TempDir so the test runner
// cleans them up automatically; no xlsx binary is committed to the repo.
package fixtures

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// Column names duplicated from internal/ingest. Keeping them here means
// fixtures/ has no inbound dependency on ingest, so a future split of the
// ingest package can't induce an import cycle via the test helpers.
const (
	colNumber          = "Number"
	colShortDesc       = "Short Description"
	colDescription     = "Description(description)"
	colState           = "State"
	colPriority        = "Priority"
	colSeverity        = "Severity"
	colUrgency         = "Urgency"
	colImpact          = "Impact"
	colAssignedTo      = "Assigned to"
	colAssignmentGroup = "Assignment Group"
	colOpened          = "Opened"
	colUpdated         = "Updated"
	colOpenedBy        = "Opened by"
	colUpdatedBy       = "Updated by"
	colCaller          = "Caller"
	colCompany         = "Company"
	colBusinessService = "Business service"
	colCreated         = "Created"
	colCreatedBy       = "Created by"
	colCategory        = "Category"
	colSubcategory     = "Subcategory"
	colDueDate         = "Due Date"
	colComments        = "Comments and Work notes"
	colJiraID          = "Jira ID"
	colJiraKey         = "Jira key"
	colJiraURL         = "Jira link URL"
	colJiraProject     = "Jira project"
	colJiraStatus      = "Jira status"
)

// headerOrder is the column order written to every fixture file. Kept
// stable across WriteSample10 and WriteSample10Modified so that any
// content-hash difference between the two fixtures stems solely from
// cell values, not from column re-ordering.
var headerOrder = []string{
	colNumber, colShortDesc, colDescription,
	colState, colPriority, colSeverity, colUrgency, colImpact,
	colAssignedTo, colAssignmentGroup,
	colOpened, colUpdated, colOpenedBy, colUpdatedBy, colCaller,
	colCompany, colBusinessService, colCreated, colCreatedBy,
	colCategory, colSubcategory, colDueDate, colComments,
	colJiraID, colJiraKey, colJiraURL, colJiraProject, colJiraStatus,
}

// Sample10Counts is the expected row-count shape after ingesting the file
// produced by WriteSample10 into a freshly-migrated database. Exported so
// integration tests can assert against it without re-deriving the counts.
//
//	Tickets:            10 distinct ticket_external_id values
//	TicketEvents:       12 journal entries (includes one body_hash pair
//	                    at the same (ticket, ts, author) on INC9900003)
//	Imports:            1
//	Clients:            5 (SyntheticClientA–E; carve-out rows are NULL)
//	BusinessServices:   9 (rows 1 and 10 share "Cloud SyntheticClientA-US Portal")
type Sample10Counts struct {
	Tickets          int
	TicketEvents     int
	Imports          int
	Clients          int
	BusinessServices int
}

// ExpectedSample10Counts returns the post-ingest row counts for the
// WriteSample10 fixture. Kept in one place so changes to the fixture
// row shape update the assertion in lockstep.
func ExpectedSample10Counts() Sample10Counts {
	return Sample10Counts{
		Tickets:          10,
		TicketEvents:     12,
		Imports:          1,
		Clients:          5,
		BusinessServices: 9,
	}
}

// WriteSample10 writes a 10-row synthesised xlsx to t.TempDir() and returns
// the file path. Rows are chosen to exercise every Business service token-
// count bucket observed in Phase 0 (2–7), the dunnhumby internal-service
// carve-out (no hyphen ⇒ ClientName nil), en-dash folding in Assignment
// Group, multi-line journal bodies, and the body_hash idem-key path
// introduced by migration 0006 (INC9900003 emits two distinct-body entries
// at the same (ticket, event_ts, author_name) triple).
func WriteSample10(tb testing.TB) string {
	tb.Helper()
	return writeFixture(tb, sample10Rows(false), "sample_10.xlsx")
}

// WriteSample10Modified writes the same 10 rows as WriteSample10 except
// INC9900001's Updated timestamp is advanced by 24 hours and a fresh
// journal entry is prepended. The content_hash therefore differs, so a
// re-ingest over the sample-10 database does NOT short-circuit. Expected
// write Summary after this ingest, assuming sample-10 was ingested first:
//
//	TicketsNew:     0   (every ticket_external_id already present)
//	TicketsUpdated: 10  (every row re-upserts, bumping source_import_id)
//	EventsNew:      1   (only the new journal entry on INC9900001 lands)
//	EventsSkipped:  12  (all prior events dedupe on body_hash)
//
// The non-zero EventsSkipped on a non-short-circuit run is also what
// triggers the warn-log emission added in ingest.Run — integration tests
// use this fixture to verify both the dedup path AND the warn-log
// behaviour in a single scenario.
func WriteSample10Modified(tb testing.TB) string {
	tb.Helper()
	return writeFixture(tb, sample10Rows(true), "sample_10_modified.xlsx")
}

func writeFixture(tb testing.TB, rows []map[string]any, fileName string) string {
	tb.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)

	for i, h := range headerOrder {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			tb.Fatalf("fixtures: coords header col %d: %v", i, err)
		}
		if err := f.SetCellStr(sheet, cell, h); err != nil {
			tb.Fatalf("fixtures: set header cell %s: %v", cell, err)
		}
	}

	// NumFmt 22 is "m/d/yy h:mm" — a short form that writes a date style
	// onto the cell. The reader uses RawCellValue: true so the display
	// format never reshapes the value; the style is here only so the cell
	// is typed as a date (the serial written into the underlying XML is
	// what the mapper converts).
	dateStyle, err := f.NewStyle(&excelize.Style{NumFmt: 22})
	if err != nil {
		tb.Fatalf("fixtures: new date style: %v", err)
	}

	for rIdx, values := range rows {
		rowNum := rIdx + 2
		for i, h := range headerOrder {
			cell, err := excelize.CoordinatesToCellName(i+1, rowNum)
			if err != nil {
				tb.Fatalf("fixtures: coords row %d col %d: %v", rowNum, i, err)
			}
			v, ok := values[h]
			if !ok || v == "" {
				continue
			}
			switch val := v.(type) {
			case string:
				if err := f.SetCellStr(sheet, cell, val); err != nil {
					tb.Fatalf("fixtures: set string cell %s: %v", cell, err)
				}
			case time.Time:
				if err := f.SetCellValue(sheet, cell, val); err != nil {
					tb.Fatalf("fixtures: set date cell %s: %v", cell, err)
				}
				if err := f.SetCellStyle(sheet, cell, cell, dateStyle); err != nil {
					tb.Fatalf("fixtures: set date style %s: %v", cell, err)
				}
			default:
				tb.Fatalf("fixtures: unsupported type for %s: %T", h, v)
			}
		}
	}

	path := filepath.Join(tb.TempDir(), fileName)
	if err := f.SaveAs(path); err != nil {
		tb.Fatalf("fixtures: save xlsx %s: %v", path, err)
	}
	return path
}

// sample10Rows returns the 10 synthesised rows. When modified=true,
// INC9900001's Updated timestamp advances and a new journal entry is
// prepended so the content_hash differs from the unmodified file.
func sample10Rows(modified bool) []map[string]any {
	ts := func(y, m, d, h, mi, s int) time.Time {
		return time.Date(y, time.Month(m), d, h, mi, s, 0, time.UTC)
	}

	// INC9900003's comments illustrate the body_hash idem-key case: two
	// distinct bodies at the same (ticket, event_ts, author_name) triple.
	// Pre-0006 the UNIQUE key would have collapsed both into one row.
	inc3Comments := "2026-04-09 14:30:00 - Workflow Bot (Automation)\n First automated status update.\n\n" +
		"2026-04-09 14:30:00 - Workflow Bot (Automation)\n Second automated update emitted in the same second.\n\n" +
		"2026-04-08 11:00:00 - Author4 (Service Desk)\n Initial triage note."

	rows := []map[string]any{
		// Row 1 — INC9900001: 4-token BS, en-dash AG, 2 journal entries.
		{
			colNumber:          "INC9900001",
			colShortDesc:       "Portal login failing",
			colDescription:     "Users intermittently unable to sign in to the web portal.",
			colState:           "In Progress",
			colPriority:        "High",
			colSeverity:        "2",
			colUrgency:         "High",
			colImpact:          "Medium",
			colAssignedTo:      "Synthetic User A",
			colAssignmentGroup: "Synthetic Support – Portal Login",
			colOpened:          ts(2026, 4, 10, 9, 14, 22),
			colUpdated:         ts(2026, 4, 11, 9, 14, 22),
			colOpenedBy:        "synth.userA",
			colUpdatedBy:       "system",
			colCaller:          "synth.userA",
			colCompany:         "",
			colBusinessService: "Cloud SyntheticClientA-US Portal",
			colCreated:         ts(2026, 4, 10, 9, 14, 22),
			colCreatedBy:       "system",
			colCategory:        "Access",
			colSubcategory:     "Login",
			colDueDate:         ts(2026, 4, 17, 9, 14, 22),
			colComments: "2026-04-11 10:00:00 - Author2 (Service Desk)\n Investigating failed logins.\n\n" +
				"2026-04-10 09:14:22 - Author1 (Service Desk)\n Initial ticket opened.",
		},
		// Row 2 — INC9900002: 5-token BS, 1 journal entry.
		{
			colNumber:          "INC9900002",
			colShortDesc:       "Batch job stuck",
			colDescription:     "Overnight batch job has been queued for over 12 hours.",
			colState:           "On Hold",
			colPriority:        "Moderate",
			colSeverity:        "3",
			colUrgency:         "Moderate",
			colImpact:          "Moderate",
			colAssignedTo:      "Synthetic User B",
			colAssignmentGroup: "Synthetic Support - Batch",
			colOpened:          ts(2026, 4, 9, 8, 30, 0),
			colUpdated:         ts(2026, 4, 9, 18, 30, 0),
			colOpenedBy:        "synth.userB",
			colUpdatedBy:       "synth.userB",
			colCaller:          "synth.userB",
			colCompany:         "",
			colBusinessService: "Cloud SyntheticClientB-UK PlatformServices Reporting",
			colCreated:         ts(2026, 4, 9, 8, 30, 0),
			colCreatedBy:       "system",
			colCategory:        "Batch",
			colSubcategory:     "Stuck",
			colDueDate:         ts(2026, 4, 16, 8, 30, 0),
			colComments:        "2026-04-09 18:30:00 - Author5 (Service Desk)\n Batch job still pending; awaiting capacity.",
		},
		// Row 3 — INC9900003: 3-token BS (no product), body_hash case.
		{
			colNumber:          "INC9900003",
			colShortDesc:       "Data sync mismatch",
			colDescription:     "Reported mismatch between source and target row counts.",
			colState:           "In Progress",
			colPriority:        "High",
			colSeverity:        "2",
			colUrgency:         "High",
			colImpact:          "High",
			colAssignedTo:      "Synthetic User C",
			colAssignmentGroup: "Synthetic Support - Data",
			colOpened:          ts(2026, 4, 8, 10, 45, 0),
			colUpdated:         ts(2026, 4, 9, 14, 30, 0),
			colOpenedBy:        "synth.userC",
			colUpdatedBy:       "automation",
			colCaller:          "synth.userC",
			colCompany:         "",
			colBusinessService: "Az SyntheticClientC-DE",
			colCreated:         ts(2026, 4, 8, 10, 45, 0),
			colCreatedBy:       "system",
			colCategory:        "Data",
			colSubcategory:     "Sync",
			colDueDate:         ts(2026, 4, 15, 10, 45, 0),
			colComments:        inc3Comments,
		},
		// Row 4 — INC9900004: internal service (dunnhumby carve-out), no journal.
		{
			colNumber:          "INC9900004",
			colShortDesc:       "Monitoring alert",
			colDescription:     "Internal monitoring triggered a low-severity alert.",
			colState:           "Closed",
			colPriority:        "Low",
			colSeverity:        "4",
			colUrgency:         "Low",
			colImpact:          "Low",
			colAssignedTo:      "",
			colAssignmentGroup: "Internal - Monitoring",
			colOpened:          ts(2026, 4, 1, 8, 0, 0),
			colUpdated:         ts(2026, 4, 2, 8, 0, 0),
			colOpenedBy:        "monitoring",
			colUpdatedBy:       "system",
			colCaller:          "monitoring",
			colCompany:         "",
			colBusinessService: "dunnhumby Enterprise Monitoring",
			colCreated:         ts(2026, 4, 1, 8, 0, 0),
			colCreatedBy:       "system",
			colCategory:        "",
			colSubcategory:     "",
			colDueDate:         ts(2026, 4, 8, 8, 0, 0),
			colComments:        "",
		},
		// Row 5 — INC9900005: 6-token BS, 1 journal entry.
		{
			colNumber:          "INC9900005",
			colShortDesc:       "Scoring pipeline slow",
			colDescription:     "Promotions scoring pipeline latency above target.",
			colState:           "Open",
			colPriority:        "Moderate",
			colSeverity:        "3",
			colUrgency:         "Moderate",
			colImpact:          "Moderate",
			colAssignedTo:      "Synthetic User D",
			colAssignmentGroup: "Synthetic Support - Scoring",
			colOpened:          ts(2026, 4, 7, 11, 0, 0),
			colUpdated:         ts(2026, 4, 8, 9, 0, 0),
			colOpenedBy:        "synth.userD",
			colUpdatedBy:       "synth.userD",
			colCaller:          "synth.userD",
			colCompany:         "",
			colBusinessService: "Cloud SyntheticClientA-US Portal Promotions Scoring",
			colCreated:         ts(2026, 4, 7, 11, 0, 0),
			colCreatedBy:       "system",
			colCategory:        "Performance",
			colSubcategory:     "Latency",
			colDueDate:         ts(2026, 4, 14, 11, 0, 0),
			colComments:        "2026-04-08 09:00:00 - Author6 (Service Desk)\n Gathering latency telemetry from the pipeline.",
		},
		// Row 6 — INC9900006: 7-token BS, 2 journal entries.
		{
			colNumber:          "INC9900006",
			colShortDesc:       "Realtime feed degraded",
			colDescription:     "Tier1 premium realtime feed throughput below threshold.",
			colState:           "In Progress",
			colPriority:        "Critical",
			colSeverity:        "1",
			colUrgency:         "High",
			colImpact:          "High",
			colAssignedTo:      "Synthetic User E",
			colAssignmentGroup: "Synthetic Support – Realtime",
			colOpened:          ts(2026, 4, 6, 7, 15, 0),
			colUpdated:         ts(2026, 4, 7, 16, 15, 0),
			colOpenedBy:        "synth.userE",
			colUpdatedBy:       "synth.userE",
			colCaller:          "synth.userE",
			colCompany:         "",
			colBusinessService: "Cloud SyntheticClientD-FR Analytics Tier1 Premium Realtime",
			colCreated:         ts(2026, 4, 6, 7, 15, 0),
			colCreatedBy:       "system",
			colCategory:        "Realtime",
			colSubcategory:     "Feed",
			colDueDate:         ts(2026, 4, 13, 7, 15, 0),
			colComments: "2026-04-07 16:15:00 - Author7 (Service Desk)\n Feed partially restored, continuing to monitor.\n\n" +
				"2026-04-06 07:15:00 - Author1 (Service Desk)\n Initial outage report received.",
		},
		// Row 7 — INC9900007: 2-token BS (Platform + single product word), no journal.
		{
			colNumber:          "INC9900007",
			colShortDesc:       "Internal ticket",
			colDescription:     "Platform-internal tracking ticket with no external client.",
			colState:           "Resolved",
			colPriority:        "Low",
			colSeverity:        "4",
			colUrgency:         "Low",
			colImpact:          "Low",
			colAssignedTo:      "Synthetic User F",
			colAssignmentGroup: "Internal - Platform",
			colOpened:          ts(2026, 4, 5, 9, 0, 0),
			colUpdated:         ts(2026, 4, 6, 9, 0, 0),
			colOpenedBy:        "synth.userF",
			colUpdatedBy:       "synth.userF",
			colCaller:          "synth.userF",
			colCompany:         "",
			colBusinessService: "Cloud Internal",
			colCreated:         ts(2026, 4, 5, 9, 0, 0),
			colCreatedBy:       "system",
			colCategory:        "Admin",
			colSubcategory:     "Tracking",
			colDueDate:         ts(2026, 4, 12, 9, 0, 0),
			colComments:        "",
		},
		// Row 8 — INC9900008: 4-token BS, no journal, all optional cols empty.
		{
			colNumber:          "INC9900008",
			colShortDesc:       "Analytics refresh failing",
			colDescription:     "Analytics dataset refresh returns an empty response.",
			colState:           "Open",
			colPriority:        "Moderate",
			colSeverity:        "3",
			colUrgency:         "Moderate",
			colImpact:          "Moderate",
			colAssignedTo:      "",
			colAssignmentGroup: "Synthetic Support - Analytics",
			colOpened:          ts(2026, 4, 4, 14, 0, 0),
			colUpdated:         ts(2026, 4, 5, 14, 0, 0),
			colOpenedBy:        "synth.userG",
			colUpdatedBy:       "synth.userG",
			colCaller:          "synth.userG",
			colCompany:         "",
			colBusinessService: "Az SyntheticClientB-UK Analytics",
			colCreated:         ts(2026, 4, 4, 14, 0, 0),
			colCreatedBy:       "system",
			colCategory:        "",
			colSubcategory:     "",
			colDueDate:         ts(2026, 4, 11, 14, 0, 0),
			colComments:        "",
		},
		// Row 9 — INC9900009: 4-token BS with en-dash AG, 1 journal entry.
		{
			colNumber:          "INC9900009",
			colShortDesc:       "Portal cache miss",
			colDescription:     "Portal pages served stale cache for logged-in users.",
			colState:           "In Progress",
			colPriority:        "Moderate",
			colSeverity:        "3",
			colUrgency:         "Moderate",
			colImpact:          "Moderate",
			colAssignedTo:      "Synthetic User H",
			colAssignmentGroup: "Synthetic Support – Portal Caching",
			colOpened:          ts(2026, 4, 3, 10, 0, 0),
			colUpdated:         ts(2026, 4, 4, 10, 0, 0),
			colOpenedBy:        "synth.userH",
			colUpdatedBy:       "synth.userH",
			colCaller:          "synth.userH",
			colCompany:         "",
			colBusinessService: "Cloud SyntheticClientE-JP Portal",
			colCreated:         ts(2026, 4, 3, 10, 0, 0),
			colCreatedBy:       "system",
			colCategory:        "Caching",
			colSubcategory:     "Stale",
			colDueDate:         ts(2026, 4, 10, 10, 0, 0),
			colComments:        "2026-04-04 10:00:00 - Author8 (Service Desk)\n Cache invalidation triggered, awaiting propagation.",
		},
		// Row 10 — INC9900010: 4-token BS (dup of row 1), 2 journal entries.
		{
			colNumber:          "INC9900010",
			colShortDesc:       "Portal timeout",
			colDescription:     "Second report of portal timeouts; possibly related to INC9900001.",
			colState:           "Open",
			colPriority:        "High",
			colSeverity:        "2",
			colUrgency:         "High",
			colImpact:          "Medium",
			colAssignedTo:      "Synthetic User A",
			colAssignmentGroup: "Synthetic Support - Portal Login",
			colOpened:          ts(2026, 4, 2, 13, 0, 0),
			colUpdated:         ts(2026, 4, 3, 13, 0, 0),
			colOpenedBy:        "synth.userI",
			colUpdatedBy:       "synth.userI",
			colCaller:          "synth.userI",
			colCompany:         "",
			colBusinessService: "Cloud SyntheticClientA-US Portal",
			colCreated:         ts(2026, 4, 2, 13, 0, 0),
			colCreatedBy:       "system",
			colCategory:        "Access",
			colSubcategory:     "Timeout",
			colDueDate:         ts(2026, 4, 9, 13, 0, 0),
			colComments: "2026-04-03 13:00:00 - Author9 (Service Desk)\n Timeout reproducible, escalating.\n\n" +
				"2026-04-02 13:00:00 - Author1 (Service Desk)\n New timeout report, linking to INC9900001.",
		},
	}

	if modified {
		rows[0][colUpdated] = ts(2026, 4, 12, 9, 14, 22)
		rows[0][colComments] = "2026-04-12 09:00:00 - Author3 (Service Desk)\n Resolution applied, monitoring.\n\n" +
			"2026-04-11 10:00:00 - Author2 (Service Desk)\n Investigating failed logins.\n\n" +
			"2026-04-10 09:14:22 - Author1 (Service Desk)\n Initial ticket opened."
	}

	return rows
}
