#!/usr/bin/env bats

setup() {
    REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
    # shellcheck source=../lib/common.sh
    source "$REPO/lib/common.sh"
    # shellcheck source=../lib/audit.sh
    source "$REPO/lib/audit.sh"
    FIX="$REPO/tests/fixtures"
}

@test "npm: clean fixture yields zero counts" {
    out=$(export AUDIT_PATH=/site/www; cat "$FIX/npm-audit-clean.json" | audit_parse_npm)
    [ "$(printf '%s' "$out" | jq -r '.status')" = "ok" ]
    [ "$(printf '%s' "$out" | jq -r '.audit_path')" = "/site/www" ]
    [ "$(printf '%s' "$out" | jq -r '.counts.high')" = "0" ]
    [ "$(printf '%s' "$out" | jq '.advisories | length')" = "0" ]
}

@test "npm: vulns fixture pulls counts from metadata" {
    out=$(export AUDIT_PATH=/site/www; cat "$FIX/npm-audit-vulns.json" | audit_parse_npm)
    [ "$(printf '%s' "$out" | jq -r '.counts.high')" = "1" ]
    [ "$(printf '%s' "$out" | jq -r '.counts.moderate')" = "1" ]
    [ "$(printf '%s' "$out" | jq '.advisories | length')" = "2" ]
    # Severity propagated.
    [ "$(printf '%s' "$out" | jq -r '[.advisories[].severity] | sort | join(",")')" = "high,moderate" ]
}

@test "npm: advisory title pulled from via[].title when object" {
    out=$(export AUDIT_PATH=/x; cat "$FIX/npm-audit-vulns.json" | audit_parse_npm)
    titles=$(printf '%s' "$out" | jq -r '.advisories[].title')
    echo "$titles" | grep -q "Prototype pollution"
}
