#!/bin/sh
# [Rule: Zero egress] container-start smoke test.
#
# Refuses to start any dh-support-assistant container that can resolve an
# external hostname. The support-net (docker/compose.yaml) is declared
# `internal: true` so the lookup MUST fail. If it succeeds, the topology is
# broken and silently starting would risk ticket data leaving the host.
#
# Uses busybox `nslookup` (always present in alpine:latest base) rather than
# `getent`, which is not in the base image's shell tools.

set -u

probe_host="${DH_EGRESS_PROBE_HOST:-example.com}"

if nslookup "$probe_host" >/dev/null 2>&1; then
    printf 'FATAL: %s resolved inside container — [Rule: Zero egress] violated; aborting\n' "$probe_host" >&2
    exit 1
fi

printf '[egress] %s not resolvable — zero-egress confirmed\n' "$probe_host" >&2

exec "$@"
