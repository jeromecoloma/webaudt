#!/usr/bin/env bats

setup() {
    REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
    # shellcheck source=../lib/common.sh
    source "$REPO/lib/common.sh"
    # shellcheck source=../lib/config.sh
    source "$REPO/lib/config.sh"
}

# Pipe JSON to config_validate. Bats `run` preserves the current shell's functions.
_validate() {
    printf '%s' "$1" | config_validate
}

@test "config: valid blob passes" {
    run _validate '{"settings":{"cache_ttl":3600,"parallel_audits":4,"color":"auto"},"sites":[{"name":"a","path":"/x","type":"both"}]}'
    [ "$status" -eq 0 ]
}

@test "config: rejects duplicate site names" {
    run _validate '{"settings":{"cache_ttl":1,"parallel_audits":1,"color":"auto"},"sites":[{"name":"a","path":"/x","type":"npm"},{"name":"a","path":"/y","type":"npm"}]}'
    [ "$status" -ne 0 ]
    [[ "$output" == *"duplicate site name: a"* ]]
}

@test "config: rejects non-absolute path" {
    run _validate '{"settings":{"cache_ttl":1,"parallel_audits":1,"color":"auto"},"sites":[{"name":"a","path":"relative","type":"npm"}]}'
    [ "$status" -ne 0 ]
    [[ "$output" == *"must be an absolute path"* ]]
}

@test "config: rejects bad type" {
    run _validate '{"settings":{"cache_ttl":1,"parallel_audits":1,"color":"auto"},"sites":[{"name":"a","path":"/x","type":"yarn"}]}'
    [ "$status" -ne 0 ]
    [[ "$output" == *"composer|npm|both"* ]]
}

@test "config: rejects parallel_audits out of range" {
    run _validate '{"settings":{"cache_ttl":1,"parallel_audits":99,"color":"auto"},"sites":[]}'
    [ "$status" -ne 0 ]
    [[ "$output" == *"parallel_audits"* ]]
}

@test "config: rejects bad color" {
    run _validate '{"settings":{"cache_ttl":1,"parallel_audits":1,"color":"rainbow"},"sites":[]}'
    [ "$status" -ne 0 ]
    [[ "$output" == *"color"* ]]
}

@test "config: empty sites array is fine" {
    run _validate '{"settings":{"cache_ttl":3600,"parallel_audits":4,"color":"auto"},"sites":[]}'
    [ "$status" -eq 0 ]
}
