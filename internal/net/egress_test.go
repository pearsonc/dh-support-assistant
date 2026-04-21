package egress

import (
	"context"
	"errors"
	"os"
	"testing"
)

// TestVerifyBlocked_Logic proves the contract without relying on network
// topology: localhost ALWAYS resolves (nss fallback to /etc/hosts), so the
// helper must return ErrEgressReachable; a syntactically-invalid TLD never
// resolves, so the helper must return nil.
func TestVerifyBlocked_Logic(t *testing.T) {
	t.Run("resolvable host returns ErrEgressReachable", func(t *testing.T) {
		err := VerifyBlocked(context.Background(), "localhost")
		if err == nil {
			t.Fatal("VerifyBlocked(localhost): expected error, got nil")
		}
		if !errors.Is(err, ErrEgressReachable) {
			t.Fatalf("VerifyBlocked(localhost): expected ErrEgressReachable, got %v", err)
		}
	})

	t.Run("unresolvable host returns nil", func(t *testing.T) {
		err := VerifyBlocked(context.Background(), "this-host-definitely-does-not-exist-9f3a2b.invalid")
		if err != nil {
			t.Fatalf("VerifyBlocked(invalid): expected nil, got %v", err)
		}
	})
}

// TestVerifyBlocked_InContainer exercises the real zero-egress topology.
// Skipped unless DH_IN_SUPPORT_NET is set — the Dockerfile sets it so this
// runs inside the app container during phase-1.5 integration tests, and
// never on the developer host where example.com is reachable.
func TestVerifyBlocked_InContainer(t *testing.T) {
	if os.Getenv("DH_IN_SUPPORT_NET") == "" {
		t.Skip("DH_IN_SUPPORT_NET unset — skipping container-only topology test")
	}

	cases := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{name: "external example.com blocked", host: DefaultProbeHost, wantErr: false},
		{name: "internal postgres reachable", host: "postgres", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyBlocked(context.Background(), tc.host)
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Fatalf("VerifyBlocked(%q): err=%v wantErr=%v", tc.host, err, tc.wantErr)
			}
		})
	}
}
