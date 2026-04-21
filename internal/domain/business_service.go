// Package domain contains pure data types and parsers that translate
// ServiceNow export values into the schema defined in migrations/. No I/O,
// no DB access — the ingest pipeline (internal/ingest) calls these during
// xlsx → row mapping so every parse rule is unit-testable without a file.
package domain

import (
	"errors"
	"strings"
)

// BusinessService is the parsed decomposition of a ServiceNow "Business
// service" cell. Phase 0 Field Inventory observed 61 distinct values with
// the grammar `{Platform} {Client}-{CountryCode} {Product...}` and token
// counts of 2–7. Internal services (e.g. "dunnhumby Enterprise Monitoring")
// lack the hyphenated client-country token — ClientName is nil for those
// rows so the schema's business_services.client_id FK stays NULL per
// migrations/0003_business_services.sql.
type BusinessService struct {
	RawValue    string
	Platform    string
	ClientName  *string
	CountryCode *string
	Product     *string
}

// ErrEmptyBusinessService is returned for empty or whitespace-only input.
// The schema marks business_services.raw_value NOT NULL so the ingest
// writer treats this as a hard failure, not a soft skip.
var ErrEmptyBusinessService = errors.New("domain: business service is empty")

// ParseBusinessService decomposes a ServiceNow Business Service cell into
// its dimensions. Algorithm:
//  1. Trim and split on whitespace.
//  2. Platform = first token.
//  3. Scan remaining tokens for the first containing '-' where the hyphen
//     is neither the first nor last character; split that token on the
//     LAST '-' so client names with internal hyphens (e.g. "Coca-Cola")
//     still yield a valid country code suffix.
//  4. All tokens AFTER the hyphenated one join with a single space to form
//     Product.
//  5. If no hyphenated token exists the input is an internal service:
//     ClientName and CountryCode stay nil; everything after Platform joins
//     into Product (itself nil if Platform is the only token).
func ParseBusinessService(raw string) (BusinessService, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return BusinessService{}, ErrEmptyBusinessService
	}
	tokens := strings.Fields(trimmed)
	bs := BusinessService{RawValue: trimmed, Platform: tokens[0]}
	if len(tokens) == 1 {
		return bs, nil
	}

	for i := 1; i < len(tokens); i++ {
		idx := strings.LastIndex(tokens[i], "-")
		if idx <= 0 || idx >= len(tokens[i])-1 {
			continue
		}
		client := tokens[i][:idx]
		country := tokens[i][idx+1:]
		bs.ClientName = &client
		bs.CountryCode = &country
		if rest := tokens[i+1:]; len(rest) > 0 {
			prod := strings.Join(rest, " ")
			bs.Product = &prod
		}
		return bs, nil
	}

	if rest := tokens[1:]; len(rest) > 0 {
		prod := strings.Join(rest, " ")
		bs.Product = &prod
	}
	return bs, nil
}
