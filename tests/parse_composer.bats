#!/usr/bin/env bats

setup() {
    REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
    # shellcheck source=../lib/common.sh
    source "$REPO/lib/common.sh"
    # shellcheck source=../lib/audit.sh
    source "$REPO/lib/audit.sh"
    FIX="$REPO/tests/fixtures"
}

@test "composer: clean fixture yields zero counts and empty advisories" {
    export AUDIT_PATH=/site
    out=$(cat "$FIX/composer-audit-clean.json" | audit_parse_composer)
    [ "$(printf '%s' "$out" | jq -r '.status')" = "ok" ]
    [ "$(printf '%s' "$out" | jq -r '.audit_path')" = "/site" ]
    [ "$(printf '%s' "$out" | jq -r '.counts.critical')" = "0" ]
    [ "$(printf '%s' "$out" | jq -r '.counts.high')" = "0" ]
    [ "$(printf '%s' "$out" | jq '.advisories | length')" = "0" ]
}

@test "composer: vulns fixture counts critical/high and normalizes medium -> moderate" {
    export AUDIT_PATH=/site
    out=$(cat "$FIX/composer-audit-vulns.json" | audit_parse_composer)
    [ "$(printf '%s' "$out" | jq -r '.counts.critical')" = "1" ]
    [ "$(printf '%s' "$out" | jq -r '.counts.high')" = "1" ]
    [ "$(printf '%s' "$out" | jq -r '.counts.moderate')" = "1" ]
    [ "$(printf '%s' "$out" | jq -r '.counts.low')" = "0" ]
    # No "medium" should leak into the normalized output.
    sevs=$(printf '%s' "$out" | jq -r '.advisories[].severity')
    ! grep -qx medium <<< "$sevs"
    grep -qx moderate <<< "$sevs"
}

@test "composer: advisories capped at AUDIT_ADVISORY_CAP" {
    # Build a synthetic fixture with > cap advisories.
    big=$(jq -n --argjson n 75 '
        { advisories: { "pkg/foo": [range(0;$n) | {advisoryId: "A\(.)", cve: "CVE-\(.)", title: "t", severity: "low", affectedVersions: "*"}] } }')
    export AUDIT_PATH=/site
    out=$(printf '%s' "$big" | audit_parse_composer)
    [ "$(printf '%s' "$out" | jq '.advisories | length')" = "50" ]
}
