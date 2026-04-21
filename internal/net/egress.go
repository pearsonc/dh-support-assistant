// Package egress enforces [Rule: Zero egress] at runtime. VerifyBlocked is
// the sole public contract: given a hostname, it MUST succeed only when DNS
// resolution for that hostname FAILS. Succeeding resolution indicates the
// container has a path off the support-net, which violates the project rule.
//
// The helper is called from the container entrypoint (indirectly, via the
// shell smoke test) and from the `go test` logic tests in egress_test.go.
package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// DefaultProbeHost is the canonical external hostname used to verify the
// network topology blocks outbound DNS. The shell entrypoint uses the same
// name so host-level manual checks stay consistent.
const DefaultProbeHost = "example.com"

// DefaultProbeTimeout bounds the lookup. Inside `support-net: internal: true`
// lookups hang until the Docker embedded DNS resolver gives up; a short
// budget here keeps container startup fast.
const DefaultProbeTimeout = 3 * time.Second

// ErrEgressReachable signals that an external hostname resolved — i.e. the
// container or host CAN reach outside. Callers should abort startup when
// they observe this error from VerifyBlocked.
var ErrEgressReachable = errors.New("external DNS resolution succeeded; [Rule: Zero egress] violated")

// VerifyBlocked resolves host with a short timeout. It returns nil when
// resolution fails (the expected outcome inside the dh-support-assistant
// stack) and an error wrapping ErrEgressReachable when resolution succeeds.
//
// The check is deliberately simple: DNS is the first and cheapest signal of
// egress. A full outbound TCP probe is unnecessary — without DNS the app
// can't reach anything outside by name anyway, which is the only interface
// sensitive data would leave the stack through.
func VerifyBlocked(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultProbeTimeout)
	defer cancel()

	var r net.Resolver
	addrs, err := r.LookupHost(ctx, host)
	if err != nil {
		return nil
	}
	return fmt.Errorf("%w: resolved %s to %v", ErrEgressReachable, host, addrs)
}
