package db

import "time"

// Go structs mirroring the schema defined in migrations/000[1-5]_*.sql.
// Field order matches column order; nullable columns use pointer types
// so pgx.RowToStructByName can scan NULL cleanly. Struct tags are
// "db:"column_name"" per pgx/v5 default field mapping.
//
// Non-pointer fields correspond to NOT NULL columns — any Phase 0 field
// observed at 100% population in the sample export. Pointer fields map
// to columns that are genuinely nullable in the source data:
//   - Ticket.AssignedTo   (79.1% populated)
//   - Ticket.CompanyRaw   (17%  populated — AUDIT ONLY, never client-scoping)
//   - Ticket.Category     (81%  populated)
//   - Ticket.Subcategory  (81%  populated)
//   - Ticket.Jira*        (0%   populated; reserved)
//   - BusinessService.ClientID/CountryCode/Product (null for internal services)

type Import struct {
	ID         int64     `db:"id"`
	ImportHash string    `db:"import_hash"`
	FileName   string    `db:"file_name"`
	RowCount   int       `db:"row_count"`
	IngestedAt time.Time `db:"ingested_at"`
}

type Client struct {
	ID          int64     `db:"id"`
	ClientName  string    `db:"client_name"`
	FirstSeenAt time.Time `db:"first_seen_at"`
}

type BusinessService struct {
	ID          int64   `db:"id"`
	RawValue    string  `db:"raw_value"`
	Platform    string  `db:"platform"`
	ClientID    *int64  `db:"client_id"`
	CountryCode *string `db:"country_code"`
	Product     *string `db:"product"`
}

type Ticket struct {
	ID                int64     `db:"id"`
	TicketExternalID  string    `db:"ticket_external_id"`
	ShortDescription  string    `db:"short_description"`
	Description       string    `db:"description"`
	State             string    `db:"state"`
	Priority          string    `db:"priority"`
	Severity          string    `db:"severity"`
	Urgency           string    `db:"urgency"`
	Impact            string    `db:"impact"`
	AssignedTo        *string   `db:"assigned_to"`
	AssignmentGroup   string    `db:"assignment_group"`
	OpenedAt          time.Time `db:"opened_at"`
	UpdatedAt         time.Time `db:"updated_at"`
	OpenedBy          string    `db:"opened_by"`
	UpdatedBy         string    `db:"updated_by"`
	Caller            string    `db:"caller"`
	CompanyRaw        *string   `db:"company_raw"`
	BusinessServiceID int64     `db:"business_service_id"`
	CreatedAt         time.Time `db:"created_at"`
	CreatedBy         string    `db:"created_by"`
	Category          *string   `db:"category"`
	Subcategory       *string   `db:"subcategory"`
	DueDate           time.Time `db:"due_date"`
	JiraID            *string   `db:"jira_id"`
	JiraKey           *string   `db:"jira_key"`
	JiraURL           *string   `db:"jira_url"`
	JiraProject       *string   `db:"jira_project"`
	JiraStatus        *string   `db:"jira_status"`
	SourceImportID    int64     `db:"source_import_id"`
}

type TicketEvent struct {
	ID             int64     `db:"id"`
	TicketID       int64     `db:"ticket_id"`
	EventTS        time.Time `db:"event_ts"`
	AuthorName     string    `db:"author_name"`
	AuthorContext  string    `db:"author_context"`
	Body           string    `db:"body"`
	SourceImportID int64     `db:"source_import_id"`
}
