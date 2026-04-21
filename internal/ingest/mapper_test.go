package ingest

import (
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestHeadersRejectsMissingRequired(t *testing.T) {
	t.Parallel()
	// Headers validates every entry in requiredCols. Drop "Opened" to
	// prove the error lists the missing name.
	raw := []string{
		colNumber, colShortDesc, colDescription,
		colState, colPriority, colSeverity, colUrgency, colImpact,
		colAssignmentGroup /* colOpened omitted */, colUpdated, colOpenedBy, colUpdatedBy, colCaller,
		colBusinessService, colCreated, colCreatedBy, colDueDate, colComments,
	}
	_, err := Headers(raw)
	if err == nil || !strings.Contains(err.Error(), colOpened) {
		t.Fatalf("expected missing-column error mentioning %q; got %v", colOpened, err)
	}
}

func TestMapRowRejectsMissingRequiredField(t *testing.T) {
	t.Parallel()
	// Build the narrowest valid column index, then blank each required
	// string field in turn. The mapper reports the first offender; a
	// missing-field case per required column isn't worth the noise — one
	// representative field proves the whole guard fires.
	cols := map[string]int{
		colNumber: 0, colShortDesc: 1, colDescription: 2,
		colState: 3, colPriority: 4, colSeverity: 5, colUrgency: 6, colImpact: 7,
		colAssignmentGroup: 8, colOpened: 9, colUpdated: 10, colOpenedBy: 11,
		colUpdatedBy: 12, colCaller: 13, colBusinessService: 14, colCreated: 15,
		colCreatedBy: 16, colDueDate: 17, colComments: 18,
	}
	base := func() []string {
		return []string{
			"INC0000999", "Short", "Desc",
			"Open", "High", "2", "High", "Medium",
			"Group", "45123.4", "45124.4", "opener", "updater", "caller",
			"Cloud X-US Y", "45123.4", "creator", "45130.4", "",
		}
	}

	for field, idx := range map[string]int{
		"ticket_external_id": 0,
		"short_description":  1,
		"state":              3,
		"caller":             13,
	} {
		field, idx := field, idx
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			row := base()
			row[idx] = ""
			_, err := MapRow(cols, row)
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("expected error mentioning %q; got %v", field, err)
			}
		})
	}
}

func TestMapRowRejectsInvalidOLESerial(t *testing.T) {
	t.Parallel()
	cols := map[string]int{
		colNumber: 0, colShortDesc: 1, colDescription: 2,
		colState: 3, colPriority: 4, colSeverity: 5, colUrgency: 6, colImpact: 7,
		colAssignmentGroup: 8, colOpened: 9, colUpdated: 10, colOpenedBy: 11,
		colUpdatedBy: 12, colCaller: 13, colBusinessService: 14, colCreated: 15,
		colCreatedBy: 16, colDueDate: 17, colComments: 18,
	}
	row := []string{
		"INC0000998", "Short", "Desc",
		"Open", "High", "2", "High", "Medium",
		"Group", "not-a-serial", "45124.4", "opener", "updater", "caller",
		"Cloud X-US Y", "45123.4", "creator", "45130.4", "",
	}
	_, err := MapRow(cols, row)
	if err == nil || !strings.Contains(err.Error(), "OLE serial") {
		t.Fatalf("expected OLE-serial error; got %v", err)
	}

	row[9] = ""
	_, err = MapRow(cols, row)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-date error; got %v", err)
	}
}

func TestHeadersAcceptsMissingOptionalColumns(t *testing.T) {
	t.Parallel()
	// Optional columns (Assigned to, Company, Category, Subcategory,
	// Jira*) are tolerated absent entirely per Phase 0 population rates.
	raw := append([]string(nil), requiredCols...)
	idx, err := Headers(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := idx[colAssignedTo]; ok {
		t.Errorf("unexpected optional column in index: %s", colAssignedTo)
	}
}

func TestMapRowHandlesFullXlsxFixture(t *testing.T) {
	t.Parallel()
	// Build a synthetic xlsx in-memory that mirrors the Phase 0 export's
	// 28-column shape. Date cells go in as native time.Time values so
	// excelize writes them as OLE serials with a date style, matching
	// what ServiceNow emits. Reader returns raw strings (RawCellValue:
	// true), mapper converts back to time.Time. That round-trip is the
	// critical handshake this test exercises.
	path := writeFixtureXlsx(t)

	file, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(file.Headers) != 28 {
		t.Fatalf("want 28 headers, got %d", len(file.Headers))
	}
	if len(file.Rows) != 2 {
		t.Fatalf("want 2 data rows, got %d", len(file.Rows))
	}

	cols, err := Headers(file.Headers)
	if err != nil {
		t.Fatalf("Headers: %v", err)
	}

	r0, err := MapRow(cols, file.Rows[0])
	if err != nil {
		t.Fatalf("MapRow row0: %v", err)
	}
	if r0.TicketExternalID != "INC0000001" {
		t.Errorf("row0 Number: got %q", r0.TicketExternalID)
	}
	// Round-trip sanity: the date written was 2026-04-10 09:14:22 UTC;
	// after OLE serial → time.Time, the mapper's UTC() call must yield
	// the same wall time back.
	wantTS := time.Date(2026, 4, 10, 9, 14, 22, 0, time.UTC)
	if !r0.OpenedAt.Equal(wantTS) {
		t.Errorf("row0 OpenedAt: got %v, want %v", r0.OpenedAt, wantTS)
	}
	// en-dash fold check: fixture's row0 Assignment Group uses en-dash.
	if !strings.Contains(r0.AssignmentGroup, " - ") || strings.Contains(r0.AssignmentGroup, "–") {
		t.Errorf("en-dash not folded: got %q", r0.AssignmentGroup)
	}
	if r0.BusinessService.ClientName == nil || *r0.BusinessService.ClientName != "ClientX" {
		t.Errorf("business_service client: got %+v", r0.BusinessService)
	}
	if len(r0.Journal) != 2 {
		t.Fatalf("want 2 journal entries, got %d", len(r0.Journal))
	}
	// Journal should be ascending by timestamp.
	if !r0.Journal[0].EventTS.Before(r0.Journal[1].EventTS) {
		t.Errorf("journal not ascending: %v, %v", r0.Journal[0].EventTS, r0.Journal[1].EventTS)
	}

	// Row 1 exercises optional column nullability (all optionals empty).
	r1, err := MapRow(cols, file.Rows[1])
	if err != nil {
		t.Fatalf("MapRow row1: %v", err)
	}
	if r1.AssignedTo != nil {
		t.Errorf("AssignedTo should be nil, got %q", *r1.AssignedTo)
	}
	if r1.Category != nil {
		t.Errorf("Category should be nil, got %q", *r1.Category)
	}
	if r1.JiraID != nil {
		t.Errorf("JiraID should be nil, got %q", *r1.JiraID)
	}
	// Internal business service carve-out (no hyphen → client nil).
	if r1.BusinessService.ClientName != nil {
		t.Errorf("internal service should have nil ClientName, got %q", *r1.BusinessService.ClientName)
	}
	if r1.BusinessService.Platform != "dunnhumby" {
		t.Errorf("internal service Platform: got %q", r1.BusinessService.Platform)
	}
}

// writeFixtureXlsx builds a two-row xlsx matching the Phase 0 column shape
// and returns its on-disk path (t.TempDir cleans up automatically).
func writeFixtureXlsx(t *testing.T) string {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)

	headers := []string{
		colNumber, colShortDesc, colDescription,
		colState, colPriority, colSeverity, colUrgency, colImpact,
		colAssignedTo, colAssignmentGroup,
		colOpened, colUpdated, colOpenedBy, colUpdatedBy, colCaller,
		colCompany, colBusinessService, colCreated, colCreatedBy,
		colCategory, colSubcategory, colDueDate, colComments,
		colJiraID, colJiraKey, colJiraURL, colJiraProject, colJiraStatus,
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellStr(sheet, cell, h); err != nil {
			t.Fatalf("SetCellStr header %d: %v", i, err)
		}
	}

	// Date style: short ISO form so the serial writes cleanly; actual
	// format doesn't affect the round-trip because we read with
	// RawCellValue: true.
	dateStyle, err := f.NewStyle(&excelize.Style{NumFmt: 22})
	if err != nil {
		t.Fatalf("NewStyle: %v", err)
	}

	// Row 2 — full client ticket with en-dash assignment group and
	// multi-entry journal.
	row2 := map[string]any{
		colNumber:          "INC0000001",
		colShortDesc:       "Example short",
		colDescription:     "Example description body.",
		colState:           "In Progress",
		colPriority:        "High",
		colSeverity:        "2",
		colUrgency:         "High",
		colImpact:          "Medium",
		colAssignedTo:      "Chris Pearson",
		colAssignmentGroup: "P&P Product Support – dhPrice Reporting", // en-dash
		colOpened:          time.Date(2026, 4, 10, 9, 14, 22, 0, time.UTC),
		colUpdated:         time.Date(2026, 4, 11, 9, 14, 22, 0, time.UTC),
		colOpenedBy:        "chris.pearson",
		colUpdatedBy:       "system",
		colCaller:          "chris.pearson",
		colCompany:         "",
		colBusinessService: "Cloud ClientX-US Datahub",
		colCreated:         time.Date(2026, 4, 10, 9, 14, 22, 0, time.UTC),
		colCreatedBy:       "system",
		colCategory:        "Data",
		colSubcategory:     "Missing",
		colDueDate:         time.Date(2026, 4, 17, 9, 14, 22, 0, time.UTC),
		colComments: "2026-04-11 10:00:00 - Author2 (Service Desk)\n Body of the second entry.\n\n" +
			"2026-04-10 09:14:22 - Author1 (Service Desk)\n Body of the first entry.",
		colJiraID:      "",
		colJiraKey:     "",
		colJiraURL:     "",
		colJiraProject: "",
		colJiraStatus:  "",
	}

	// Row 3 — internal service (no hyphen) + no optional columns.
	row3 := map[string]any{
		colNumber:          "INC0000002",
		colShortDesc:       "Internal tick",
		colDescription:     "Internal ticket body.",
		colState:           "Open",
		colPriority:        "Low",
		colSeverity:        "4",
		colUrgency:         "Low",
		colImpact:          "Low",
		colAssignedTo:      "",
		colAssignmentGroup: "Internal - Monitoring",
		colOpened:          time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
		colUpdated:         time.Date(2026, 4, 2, 8, 0, 0, 0, time.UTC),
		colOpenedBy:        "monitoring",
		colUpdatedBy:       "system",
		colCaller:          "monitoring",
		colCompany:         "",
		colBusinessService: "dunnhumby Enterprise Monitoring",
		colCreated:         time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
		colCreatedBy:       "system",
		colCategory:        "",
		colSubcategory:     "",
		colDueDate:         time.Date(2026, 4, 8, 8, 0, 0, 0, time.UTC),
		colComments:        "",
		colJiraID:          "",
		colJiraKey:         "",
		colJiraURL:         "",
		colJiraProject:     "",
		colJiraStatus:      "",
	}

	writeRow := func(rowNum int, values map[string]any) {
		for i, h := range headers {
			cell, _ := excelize.CoordinatesToCellName(i+1, rowNum)
			v, ok := values[h]
			if !ok || v == "" {
				continue
			}
			switch val := v.(type) {
			case string:
				if err := f.SetCellStr(sheet, cell, val); err != nil {
					t.Fatalf("SetCellStr %s: %v", cell, err)
				}
			case time.Time:
				if err := f.SetCellValue(sheet, cell, val); err != nil {
					t.Fatalf("SetCellValue %s: %v", cell, err)
				}
				if err := f.SetCellStyle(sheet, cell, cell, dateStyle); err != nil {
					t.Fatalf("SetCellStyle %s: %v", cell, err)
				}
			default:
				t.Fatalf("unsupported fixture type for %s: %T", h, v)
			}
		}
	}
	writeRow(2, row2)
	writeRow(3, row3)

	path := t.TempDir() + "/fixture.xlsx"
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	return path
}
