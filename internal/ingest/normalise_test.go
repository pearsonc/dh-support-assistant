package ingest

import "testing"

func TestNormaliseCell(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"en-dash folded", "P&P Product Support – dhPrice Reporting", "P&P Product Support - dhPrice Reporting"},
		{"multiple en-dashes", "a – b – c", "a - b - c"},
		{"trailing whitespace stripped", "value   \r\n", "value"},
		{"trailing CR stripped", "value\r", "value"},
		{"leading whitespace preserved for journal bodies", " indented body line", " indented body line"},
		{"empty unchanged", "", ""},
		{"whitespace-only stripped", "   \t\r\n", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormaliseCell(tc.in); got != tc.want {
				t.Errorf("NormaliseCell(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestContentHashDeterministic(t *testing.T) {
	t.Parallel()
	// Same logical content → same hash. The plan's idempotency acceptance
	// check ("second run inserts 0 rows") depends on this property.
	rows := [][]string{
		{"Number", "Assignment Group"},
		{"INC0000001", "P&P Product Support - Promotions"},
	}
	h1 := ContentHash(rows)
	h2 := ContentHash(rows)
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash length: got %d, want 64 (sha256 hex)", len(h1))
	}
}

func TestContentHashNormalisesBeforeHashing(t *testing.T) {
	t.Parallel()
	// Two versions of the same data, one with an en-dash where the other
	// has a hyphen, MUST produce the same hash so a cosmetic difference
	// in one sub-group does not defeat idempotency.
	rowsA := [][]string{
		{"Number", "Assignment Group"},
		{"INC0000001", "P&P Product Support – dhPrice Reporting"}, // en-dash
	}
	rowsB := [][]string{
		{"Number", "Assignment Group"},
		{"INC0000001", "P&P Product Support - dhPrice Reporting"}, // hyphen
	}
	if ContentHash(rowsA) != ContentHash(rowsB) {
		t.Errorf("en-dash vs hyphen produced different hashes; normalisation bypassed")
	}
}

func TestContentHashDifferentiatesDistinctContent(t *testing.T) {
	t.Parallel()
	rowsA := [][]string{{"a"}, {"b"}}
	rowsB := [][]string{{"a"}, {"c"}}
	if ContentHash(rowsA) == ContentHash(rowsB) {
		t.Errorf("distinct content produced identical hash")
	}
}
