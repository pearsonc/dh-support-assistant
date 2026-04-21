package api

import "github.com/pearsonc/dh-support-assistant/internal/queries"

// Wire envelopes for list responses. Keeping these as named structs rather
// than `map[string]any` gives the OpenAPI generator (and web-side TS
// generator in Phase 2.4) a stable schema to lock against.

// PageMeta is the meta block returned with paginated list responses.
type PageMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// TicketListEnvelope wraps a page of tickets.
type TicketListEnvelope struct {
	Data []queries.TicketListItem `json:"data"`
	Meta PageMeta                 `json:"meta"`
}

// TicketDetailEnvelope pairs a ticket with its timeline.
type TicketDetailEnvelope struct {
	Ticket queries.TicketDetail      `json:"ticket"`
	Events []queries.TicketEventItem `json:"events"`
}

// ClientListEnvelope wraps the client roll-up. No Meta yet — list is small
// (Phase 0 sample has 12 clients); pagination comes if the dataset grows.
type ClientListEnvelope struct {
	Data []queries.ClientRollup `json:"data"`
}

// MarketListEnvelope wraps the market roll-up. Country_code is *string so
// the internal-services bucket surfaces as `null` rather than a silent drop.
type MarketListEnvelope struct {
	Data []queries.MarketRollup `json:"data"`
}
