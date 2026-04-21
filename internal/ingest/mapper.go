package ingest

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/pearsonc/dh-support-assistant/internal/domain"
)

// Column-name constants sourced from the Phase 0 Field Inventory and
// verified against the actual xlsx header row during Phase 1.4 pre-flight.
// The header literal for Description carries "(description)" because
// ServiceNow appends the backend column name to custom-labelled fields;
// the mapper treats that as the exact string to look up.
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

// requiredCols is the set of columns whose presence the mapper insists on.
// Optional columns (Assigned to, Company, Category, Subcategory, Jira*) are
// allowed to be missing entirely per Phase 0 population rates (0–81%).
var requiredCols = []string{
	colNumber, colShortDesc, colDescription,
	colState, colPriority, colSeverity, colUrgency, colImpact,
	colAssignmentGroup, colOpened, colUpdated, colOpenedBy, colUpdatedBy, colCaller,
	colBusinessService, colCreated, colCreatedBy, colDueDate, colComments,
}

// Record is one logical ServiceNow incident plus its parsed journal and
// business-service decomposition. Foreign keys (business_service_id,
// source_import_id) are NOT assigned here — the writer sets them after
// upserting parent rows. Holding them on a pure-data struct keeps the
// mapper independent of DB state so unit tests exercise parsing without a
// Postgres instance.
type Record struct {
	TicketExternalID string
	ShortDescription string
	Description      string
	State            string
	Priority         string
	Severity         string
	Urgency          string
	Impact           string
	AssignedTo       *string
	AssignmentGroup  string
	OpenedAt         time.Time
	UpdatedAt        time.Time
	OpenedBy         string
	UpdatedBy        string
	Caller           string
	CompanyRaw       *string
	CreatedAt        time.Time
	CreatedBy        string
	Category         *string
	Subcategory      *string
	DueDate          time.Time
	JiraID           *string
	JiraKey          *string
	JiraURL          *string
	JiraProject      *string
	JiraStatus       *string

	BusinessService domain.BusinessService
	Journal         []domain.JournalEntry
}

// Headers builds a column-name → index map and validates that every
// required column is present. Missing optionals are not an error — only
// the columns listed in requiredCols are. The mapper then does a constant
// lookup per cell instead of walking the header slice per row.
func Headers(raw []string) (map[string]int, error) {
	idx := make(map[string]int, len(raw))
	for i, name := range raw {
		idx[name] = i
	}
	var missing []string
	for _, req := range requiredCols {
		if _, ok := idx[req]; !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("ingest: header row missing required columns: %s",
			strings.Join(missing, ", "))
	}
	return idx, nil
}

// MapRow converts one normalised xlsx row (padded to header width by the
// reader) into a Record. Date columns are read as OLE Automation serial
// strings (RawCellValue was true at read time) and converted via
// excelize.ExcelDateToTime so locale-dependent display formats cannot
// reshape the value between excelize's formatter and our schema.
func MapRow(cols map[string]int, row []string) (Record, error) {
	r := Record{
		TicketExternalID: cell(cols, row, colNumber),
		ShortDescription: cell(cols, row, colShortDesc),
		Description:      cell(cols, row, colDescription),
		State:            cell(cols, row, colState),
		Priority:         cell(cols, row, colPriority),
		Severity:         cell(cols, row, colSeverity),
		Urgency:          cell(cols, row, colUrgency),
		Impact:           cell(cols, row, colImpact),
		AssignedTo:       optCell(cols, row, colAssignedTo),
		AssignmentGroup:  NormaliseCell(cell(cols, row, colAssignmentGroup)),
		OpenedBy:         cell(cols, row, colOpenedBy),
		UpdatedBy:        cell(cols, row, colUpdatedBy),
		Caller:           cell(cols, row, colCaller),
		CompanyRaw:       optCell(cols, row, colCompany),
		CreatedBy:        cell(cols, row, colCreatedBy),
		Category:         optCell(cols, row, colCategory),
		Subcategory:      optCell(cols, row, colSubcategory),
		JiraID:           optCell(cols, row, colJiraID),
		JiraKey:          optCell(cols, row, colJiraKey),
		JiraURL:          optCell(cols, row, colJiraURL),
		JiraProject:      optCell(cols, row, colJiraProject),
		JiraStatus:       optCell(cols, row, colJiraStatus),
	}

	required := []struct {
		name string
		val  string
	}{
		{"ticket_external_id", r.TicketExternalID},
		{"short_description", r.ShortDescription},
		{"description", r.Description},
		{"state", r.State},
		{"priority", r.Priority},
		{"severity", r.Severity},
		{"urgency", r.Urgency},
		{"impact", r.Impact},
		{"assignment_group", r.AssignmentGroup},
		{"opened_by", r.OpenedBy},
		{"updated_by", r.UpdatedBy},
		{"caller", r.Caller},
		{"created_by", r.CreatedBy},
	}
	for _, f := range required {
		if f.val == "" {
			return Record{}, fmt.Errorf("ingest: row %q missing required column %s",
				r.TicketExternalID, f.name)
		}
	}

	var err error
	if r.OpenedAt, err = cellDate(cols, row, colOpened); err != nil {
		return Record{}, fmt.Errorf("ingest: row %q: opened_at: %w", r.TicketExternalID, err)
	}
	if r.UpdatedAt, err = cellDate(cols, row, colUpdated); err != nil {
		return Record{}, fmt.Errorf("ingest: row %q: updated_at: %w", r.TicketExternalID, err)
	}
	if r.CreatedAt, err = cellDate(cols, row, colCreated); err != nil {
		return Record{}, fmt.Errorf("ingest: row %q: created_at: %w", r.TicketExternalID, err)
	}
	if r.DueDate, err = cellDate(cols, row, colDueDate); err != nil {
		return Record{}, fmt.Errorf("ingest: row %q: due_date: %w", r.TicketExternalID, err)
	}

	r.BusinessService, err = domain.ParseBusinessService(cell(cols, row, colBusinessService))
	if err != nil {
		return Record{}, fmt.Errorf("ingest: row %q: parse business service: %w", r.TicketExternalID, err)
	}

	r.Journal, err = domain.ParseJournal(cell(cols, row, colComments))
	if err != nil {
		return Record{}, fmt.Errorf("ingest: row %q: parse journal: %w", r.TicketExternalID, err)
	}

	return r, nil
}

func cell(cols map[string]int, row []string, name string) string {
	i, ok := cols[name]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}

func optCell(cols map[string]int, row []string, name string) *string {
	v := cell(cols, row, name)
	if v == "" {
		return nil
	}
	return &v
}

// cellDate converts an OLE Automation serial ("45123.418055555555") to a
// UTC time.Time. use1904=false matches the default Windows 1900 date
// system; the Phase 0 sample was confirmed to use the 1900 system during
// the sub-phase 1.4 pre-flight xlsx inspection.
func cellDate(cols map[string]int, row []string, name string) (time.Time, error) {
	raw := cell(cols, row, name)
	if raw == "" {
		return time.Time{}, fmt.Errorf("%s is empty", name)
	}
	serial, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s is not an OLE serial %q: %w", name, raw, err)
	}
	t, err := excelize.ExcelDateToTime(serial, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: convert serial %v: %w", name, serial, err)
	}
	return t.UTC(), nil
}
