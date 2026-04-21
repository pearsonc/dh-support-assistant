package ingest

import (
	"sort"
	"testing"

	"github.com/pearsonc/dh-support-assistant/internal/ingest/fixtures"
)

// TestSample10FixtureRoundTrip exercises the full Read → Headers → MapRow
// chain over the synthesised 10-row fixture used by the integration test.
// This keeps the fixture shape validated without Docker, so a fixture
// regression shows up in `make test` rather than only in
// `make test-integration`.
func TestSample10FixtureRoundTrip(t *testing.T) {
	t.Parallel()

	path := fixtures.WriteSample10(t)
	file, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := len(file.Rows), 10; got != want {
		t.Fatalf("rows: got %d, want %d", got, want)
	}

	cols, err := Headers(file.Headers)
	if err != nil {
		t.Fatalf("Headers: %v", err)
	}

	totalEvents := 0
	clients := map[string]struct{}{}
	distinctBS := map[string]struct{}{}
	var inc3 Record
	var inc4 Record
	var inc7 Record

	for i, row := range file.Rows {
		rec, err := MapRow(cols, row)
		if err != nil {
			t.Fatalf("MapRow row %d: %v", i, err)
		}
		totalEvents += len(rec.Journal)
		distinctBS[rec.BusinessService.RawValue] = struct{}{}
		if rec.BusinessService.ClientName != nil {
			clients[*rec.BusinessService.ClientName] = struct{}{}
		}

		// Journal should be ascending by timestamp per ParseJournal's sort.
		for j := 1; j < len(rec.Journal); j++ {
			if rec.Journal[j].EventTS.Before(rec.Journal[j-1].EventTS) {
				t.Errorf("row %d: journal not ascending at index %d", i, j)
			}
		}
		// En-dash fold check on every row's Assignment Group.
		for _, r := range rec.AssignmentGroup {
			if r == '–' {
				t.Errorf("row %d: en-dash not folded in AssignmentGroup %q", i, rec.AssignmentGroup)
			}
		}

		switch rec.TicketExternalID {
		case "INC9900003":
			inc3 = rec
		case "INC9900004":
			inc4 = rec
		case "INC9900007":
			inc7 = rec
		}
	}

	if got, want := totalEvents, 12; got != want {
		t.Errorf("total journal entries: got %d, want %d", got, want)
	}
	if got, want := len(distinctBS), 9; got != want {
		t.Errorf("distinct business services: got %d, want %d (rows 1 and 10 should share)", got, want)
	}
	// SyntheticClientA..E. Row 4 is dunnhumby (no client) and row 7 is
	// Cloud Internal (no client) — both should leave clients map alone.
	wantClients := []string{"SyntheticClientA", "SyntheticClientB", "SyntheticClientC", "SyntheticClientD", "SyntheticClientE"}
	sort.Strings(wantClients)
	gotClients := make([]string, 0, len(clients))
	for c := range clients {
		gotClients = append(gotClients, c)
	}
	sort.Strings(gotClients)
	if len(gotClients) != len(wantClients) {
		t.Errorf("distinct clients: got %v, want %v", gotClients, wantClients)
	} else {
		for i, c := range wantClients {
			if gotClients[i] != c {
				t.Errorf("client %d: got %q, want %q", i, gotClients[i], c)
			}
		}
	}

	// INC9900003 must have 3 journal entries, two sharing timestamp+author.
	if len(inc3.Journal) != 3 {
		t.Fatalf("INC9900003 journal: got %d entries, want 3", len(inc3.Journal))
	}
	dup := 0
	for i := 0; i < len(inc3.Journal); i++ {
		for j := i + 1; j < len(inc3.Journal); j++ {
			if inc3.Journal[i].EventTS.Equal(inc3.Journal[j].EventTS) &&
				inc3.Journal[i].AuthorName == inc3.Journal[j].AuthorName &&
				inc3.Journal[i].Body != inc3.Journal[j].Body {
				dup++
			}
		}
	}
	if dup != 1 {
		t.Errorf("INC9900003 body_hash case: got %d same-ts+author distinct-body pairs, want 1", dup)
	}

	// INC9900004 exercises the dunnhumby internal-service carve-out.
	if inc4.BusinessService.ClientName != nil {
		t.Errorf("INC9900004 ClientName: got %v, want nil (internal service)", inc4.BusinessService.ClientName)
	}
	if inc4.BusinessService.Platform != "dunnhumby" {
		t.Errorf("INC9900004 Platform: got %q, want %q", inc4.BusinessService.Platform, "dunnhumby")
	}
	if len(inc4.Journal) != 0 {
		t.Errorf("INC9900004 Journal: got %d entries, want 0", len(inc4.Journal))
	}

	// INC9900007 is the 2-token "Cloud Internal" edge case.
	if inc7.BusinessService.ClientName != nil {
		t.Errorf("INC9900007 ClientName: got %v, want nil (2-token platform+product)", inc7.BusinessService.ClientName)
	}
	if inc7.BusinessService.Platform != "Cloud" {
		t.Errorf("INC9900007 Platform: got %q, want %q", inc7.BusinessService.Platform, "Cloud")
	}
	if inc7.BusinessService.Product == nil || *inc7.BusinessService.Product != "Internal" {
		t.Errorf("INC9900007 Product: got %v, want %q", inc7.BusinessService.Product, "Internal")
	}
}

func TestSample10ExpectedCountsMatchFixture(t *testing.T) {
	t.Parallel()
	// ExpectedSample10Counts is the contract between fixture authors and
	// integration tests. Assert it matches the shape above so changing the
	// fixture without updating the expectation is a unit-test failure.
	want := fixtures.ExpectedSample10Counts()
	if want.Tickets != 10 || want.TicketEvents != 12 || want.Imports != 1 ||
		want.Clients != 5 || want.BusinessServices != 9 {
		t.Errorf("ExpectedSample10Counts drift: %+v", want)
	}
}
