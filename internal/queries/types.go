package queries

import "time"

// Types carry DB rows out to the handler layer. JSON tags on each field are
// the authoritative wire format — handlers marshal these directly into JSON
// envelopes (see internal/api). Pointer types mark columns nullable in the
// schema (schema.go convention). When a field is projected via a LEFT JOIN
// that may miss, use a pointer even if the underlying column is NOT NULL.
//
// `db:"-"` tags on PriorityScore / Stale are deliberate: pgx's RowToStructByName
// fails in strict mode when a struct field has no matching column, so fields
// populated post-scan (in Go) must be explicitly ignored by the scanner.

// TicketListItem is one row of GET /api/tickets. Phase 2.2 adds
// PriorityScore + Stale, computed at query time by internal/scoring.
type TicketListItem struct {
	ID                int64     `db:"id"                  json:"id"`
	TicketExternalID  string    `db:"ticket_external_id"  json:"ticket_external_id"`
	ShortDescription  string    `db:"short_description"   json:"short_description"`
	State             string    `db:"state"               json:"state"`
	Severity          string    `db:"severity"            json:"severity"`
	Priority          string    `db:"priority"            json:"priority"`
	AssignedTo        *string   `db:"assigned_to"         json:"assigned_to"`
	AssignmentGroup   string    `db:"assignment_group"    json:"assignment_group"`
	OpenedAt          time.Time `db:"opened_at"           json:"opened_at"`
	UpdatedAt         time.Time `db:"updated_at"          json:"updated_at"`
	DueDate           time.Time `db:"due_date"            json:"due_date"`
	BusinessServiceID int64     `db:"business_service_id" json:"business_service_id"`
	ClientID          *int64    `db:"client_id"           json:"client_id"`
	ClientName        *string   `db:"client_name"         json:"client_name"`
	CountryCode       *string   `db:"country_code"        json:"country_code"`
	Product           *string   `db:"product"             json:"product"`
	PriorityScore     float64   `db:"-"                   json:"priority_score"`
	Stale             bool      `db:"-"                   json:"stale"`
}

// TicketListResult bundles the page + the total so handlers can render a
// consistent `{data, meta}` envelope without a second COUNT(*) round-trip.
// Total is the post-filter, pre-pagination count.
type TicketListResult struct {
	Items []TicketListItem
	Total int
}

// TicketDetail is GET /api/tickets/{id}. Adds description, caller, category,
// urgency, impact, and the Jira fields — everything the detail view needs
// without hitting the timeline table.
type TicketDetail struct {
	ID                int64     `db:"id"                  json:"id"`
	TicketExternalID  string    `db:"ticket_external_id"  json:"ticket_external_id"`
	ShortDescription  string    `db:"short_description"   json:"short_description"`
	Description       string    `db:"description"         json:"description"`
	State             string    `db:"state"               json:"state"`
	Priority          string    `db:"priority"            json:"priority"`
	Severity          string    `db:"severity"            json:"severity"`
	Urgency           string    `db:"urgency"             json:"urgency"`
	Impact            string    `db:"impact"              json:"impact"`
	AssignedTo        *string   `db:"assigned_to"         json:"assigned_to"`
	AssignmentGroup   string    `db:"assignment_group"    json:"assignment_group"`
	OpenedAt          time.Time `db:"opened_at"           json:"opened_at"`
	UpdatedAt         time.Time `db:"updated_at"          json:"updated_at"`
	OpenedBy          string    `db:"opened_by"           json:"opened_by"`
	UpdatedBy         string    `db:"updated_by"          json:"updated_by"`
	Caller            string    `db:"caller"              json:"caller"`
	CompanyRaw        *string   `db:"company_raw"         json:"company_raw"`
	BusinessServiceID int64     `db:"business_service_id" json:"business_service_id"`
	CreatedAt         time.Time `db:"created_at"          json:"created_at"`
	CreatedBy         string    `db:"created_by"          json:"created_by"`
	Category          *string   `db:"category"            json:"category"`
	Subcategory       *string   `db:"subcategory"         json:"subcategory"`
	DueDate           time.Time `db:"due_date"            json:"due_date"`
	JiraID            *string   `db:"jira_id"             json:"jira_id"`
	JiraKey           *string   `db:"jira_key"            json:"jira_key"`
	JiraURL           *string   `db:"jira_url"            json:"jira_url"`
	JiraProject       *string   `db:"jira_project"        json:"jira_project"`
	JiraStatus        *string   `db:"jira_status"         json:"jira_status"`
	ClientID          *int64    `db:"client_id"           json:"client_id"`
	ClientName        *string   `db:"client_name"         json:"client_name"`
	CountryCode       *string   `db:"country_code"        json:"country_code"`
	Product           *string   `db:"product"             json:"product"`
	PriorityScore     float64   `db:"-"                   json:"priority_score"`
	Stale             bool      `db:"-"                   json:"stale"`
}

// TicketEventItem is one row of the timeline nested inside GET
// /api/tickets/{id}. body_hash is deliberately not exposed — it's an
// ingest-side idempotency key with no UI value.
type TicketEventItem struct {
	ID            int64     `db:"id"             json:"id"`
	EventTS       time.Time `db:"event_ts"       json:"event_ts"`
	AuthorName    string    `db:"author_name"    json:"author_name"`
	AuthorContext string    `db:"author_context" json:"author_context"`
	Body          string    `db:"body"           json:"body"`
}

// TicketDetailResult bundles the ticket with its timeline.
type TicketDetailResult struct {
	Ticket TicketDetail
	Events []TicketEventItem
}

// ClientRollup is one row of GET /api/clients. ticket_count and
// business_service_count are aggregates over live tickets.
type ClientRollup struct {
	ID                   int64     `db:"id"                     json:"id"`
	ClientName           string    `db:"client_name"            json:"client_name"`
	FirstSeenAt          time.Time `db:"first_seen_at"          json:"first_seen_at"`
	TicketCount          int       `db:"ticket_count"           json:"ticket_count"`
	BusinessServiceCount int       `db:"business_service_count" json:"business_service_count"`
}

// MarketRollup is one row of GET /api/markets. country_code is the
// reliable dimension (see Phase 0 field inventory); the zero value
// (country_code = NULL, internal services) surfaces as its own row
// labelled "internal" by the handler.
type MarketRollup struct {
	CountryCode          *string `db:"country_code"           json:"country_code"`
	ClientCount          int     `db:"client_count"           json:"client_count"`
	BusinessServiceCount int     `db:"business_service_count" json:"business_service_count"`
	TicketCount          int     `db:"ticket_count"           json:"ticket_count"`
}

// StatsResult is GET /api/stats. Phase 2.2 adds PriorityBuckets (five-way
// split over scoring.BucketFor) and StaleCount (tickets flagged by
// scoring.StaleAsOf). WeightsInUse surfaces the runtime weights so the
// dashboard can explain "why did this ticket rank here?" without a
// round-trip to config.
type StatsResult struct {
	TicketsTotal          int            `json:"tickets_total"`
	TicketsByState        map[string]int `json:"tickets_by_state"`
	TicketsBySeverity     map[string]int `json:"tickets_by_severity"`
	ClientsTotal          int            `json:"clients_total"`
	BusinessServicesTotal int            `json:"business_services_total"`
	LastImport            *LastImport    `json:"last_import"`
	PriorityBuckets       map[string]int `json:"priority_buckets"`
	StaleCount            int            `json:"stale_count"`
	WeightsInUse          StatsWeights   `json:"weights_in_use"`
	StaleThresholdDays    int            `json:"stale_threshold_days"`
}

// StatsWeights is the transparent serialisation of scoring.Weights on the
// /api/stats response. Kept as its own type so a koanf rename never leaks
// through to the wire format silently.
type StatsWeights struct {
	Severity float64 `json:"severity"`
	Age      float64 `json:"age"`
	Due      float64 `json:"due"`
}

// LastImport surfaces the most recent ingest run's fingerprint so the UI
// can indicate data freshness without a second endpoint call.
type LastImport struct {
	ID         int64     `db:"id"          json:"id"`
	FileName   string    `db:"file_name"   json:"file_name"`
	RowCount   int       `db:"row_count"   json:"row_count"`
	IngestedAt time.Time `db:"ingested_at" json:"ingested_at"`
}
