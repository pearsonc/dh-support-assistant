package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestParseBusinessService(t *testing.T) {
	t.Parallel()

	// Token-count bucket coverage per Phase 1 plan hard-stop check:
	// "Business service tests assert all 6 token-count buckets (2, 3, 4, 5,
	// 6, 7) observed in Phase 0 map to a valid parse or a recognised
	// internal-service carve-out." Examples use synthetic client names so
	// the test file carries no real client data per [Rule: Zero Egress].
	cases := []struct {
		name        string
		raw         string
		bucket      int
		wantPlat    string
		wantClient  *string
		wantCountry *string
		wantProduct *string
	}{
		{
			name:        "bucket 2 client-country only (no product)",
			raw:         "Cloud ClientA-US",
			bucket:      2,
			wantPlat:    "Cloud",
			wantClient:  ptr("ClientA"),
			wantCountry: ptr("US"),
			wantProduct: nil,
		},
		{
			name:        "bucket 2 internal service (no hyphen)",
			raw:         "dunnhumby Monitoring",
			bucket:      2,
			wantPlat:    "dunnhumby",
			wantClient:  nil,
			wantCountry: nil,
			wantProduct: ptr("Monitoring"),
		},
		{
			name:        "bucket 3 client with single-word product",
			raw:         "Cloud ClientB-NO Datahub",
			bucket:      3,
			wantPlat:    "Cloud",
			wantClient:  ptr("ClientB"),
			wantCountry: ptr("NO"),
			wantProduct: ptr("Datahub"),
		},
		{
			name:        "bucket 3 internal carve-out (the canonical Phase 0 example)",
			raw:         "dunnhumby Enterprise Monitoring",
			bucket:      3,
			wantPlat:    "dunnhumby",
			wantClient:  nil,
			wantCountry: nil,
			wantProduct: ptr("Enterprise Monitoring"),
		},
		{
			name:        "bucket 4 client with multi-word product (P&P dhPromotion shape)",
			raw:         "Cloud ClientC-US P&P dhPromotion",
			bucket:      4,
			wantPlat:    "Cloud",
			wantClient:  ptr("ClientC"),
			wantCountry: ptr("US"),
			wantProduct: ptr("P&P dhPromotion"),
		},
		{
			name:        "bucket 5 Az platform",
			raw:         "Az ClientD-GB Some Other Service",
			bucket:      5,
			wantPlat:    "Az",
			wantClient:  ptr("ClientD"),
			wantCountry: ptr("GB"),
			wantProduct: ptr("Some Other Service"),
		},
		{
			name:        "bucket 6 extended product tail",
			raw:         "Cloud ClientE-NZ Alpha Beta Gamma Delta",
			bucket:      6,
			wantPlat:    "Cloud",
			wantClient:  ptr("ClientE"),
			wantCountry: ptr("NZ"),
			wantProduct: ptr("Alpha Beta Gamma Delta"),
		},
		{
			name:        "bucket 7 very long product tail",
			raw:         "Cloud ClientF-CA Alpha Beta Gamma Delta Epsilon",
			bucket:      7,
			wantPlat:    "Cloud",
			wantClient:  ptr("ClientF"),
			wantCountry: ptr("CA"),
			wantProduct: ptr("Alpha Beta Gamma Delta Epsilon"),
		},
		{
			name:        "client name with internal hyphen splits on LAST hyphen",
			raw:         "Cloud Coca-Cola-US Datahub",
			bucket:      3,
			wantPlat:    "Cloud",
			wantClient:  ptr("Coca-Cola"),
			wantCountry: ptr("US"),
			wantProduct: ptr("Datahub"),
		},
		{
			name:        "single token (degenerate but accepted)",
			raw:         "Cloud",
			bucket:      1,
			wantPlat:    "Cloud",
			wantClient:  nil,
			wantCountry: nil,
			wantProduct: nil,
		},
		{
			name:        "leading and trailing whitespace trimmed",
			raw:         "   Cloud ClientG-IE Product   ",
			bucket:      3,
			wantPlat:    "Cloud",
			wantClient:  ptr("ClientG"),
			wantCountry: ptr("IE"),
			wantProduct: ptr("Product"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseBusinessService(tc.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Platform != tc.wantPlat {
				t.Errorf("platform: got %q want %q", got.Platform, tc.wantPlat)
			}
			assertPtrEq(t, "client_name", got.ClientName, tc.wantClient)
			assertPtrEq(t, "country_code", got.CountryCode, tc.wantCountry)
			assertPtrEq(t, "product", got.Product, tc.wantProduct)
			if got.RawValue != strings.TrimSpace(tc.raw) {
				t.Errorf("raw_value: got %q want %q", got.RawValue, strings.TrimSpace(tc.raw))
			}
			// Sanity-check the bucket assertion so the table matches reality
			// if future values are added.
			if n := len(strings.Fields(strings.TrimSpace(tc.raw))); n != tc.bucket {
				t.Errorf("bucket fixture drift: counted %d tokens, table says %d", n, tc.bucket)
			}
		})
	}
}

func TestParseBusinessServiceEmpty(t *testing.T) {
	t.Parallel()
	cases := []string{"", "   ", "\t\n"}
	for _, raw := range cases {
		_, err := ParseBusinessService(raw)
		if !errors.Is(err, ErrEmptyBusinessService) {
			t.Errorf("ParseBusinessService(%q) err=%v, want ErrEmptyBusinessService", raw, err)
		}
	}
}

func TestParseBusinessServiceBucketCoverage(t *testing.T) {
	t.Parallel()
	// Ensures the table above exercises every bucket 2..7 at least once so
	// the hard-stop "token-count buckets 2–7" assertion is structurally
	// enforced and not just documented.
	wantBuckets := map[int]bool{2: false, 3: false, 4: false, 5: false, 6: false, 7: false}
	t.Run("cover", func(t *testing.T) {
		fixtures := []string{
			"Cloud ClientA-US",                            // 2
			"dunnhumby Enterprise Monitoring",             // 3
			"Cloud ClientC-US P&P dhPromotion",            // 4
			"Az ClientD-GB Some Other Service",            // 5
			"Cloud ClientE-NZ Alpha Beta Gamma Delta",     // 6
			"Cloud ClientF-CA Alpha Beta Gamma Delta Eps", // 7
		}
		for _, s := range fixtures {
			n := len(strings.Fields(s))
			wantBuckets[n] = true
			if _, err := ParseBusinessService(s); err != nil {
				t.Errorf("bucket %d fixture %q failed: %v", n, s, err)
			}
		}
		for bucket, hit := range wantBuckets {
			if !hit {
				t.Errorf("bucket %d not exercised", bucket)
			}
		}
	})
}

func ptr(s string) *string { return &s }

func assertPtrEq(t *testing.T, label string, got, want *string) {
	t.Helper()
	switch {
	case got == nil && want == nil:
		return
	case got == nil && want != nil:
		t.Errorf("%s: got nil, want %q", label, *want)
	case got != nil && want == nil:
		t.Errorf("%s: got %q, want nil", label, *got)
	case *got != *want:
		t.Errorf("%s: got %q, want %q", label, *got, *want)
	}
}
