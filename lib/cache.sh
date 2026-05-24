#!/usr/bin/env bash
# cache.sh — per-site audit result cache + TTL + advisory locks.

cache_file_for() {
    local root_path="$1"
    printf '%s/%s.json' "$(common_cache_sites_dir)" "$(common_hash "$root_path")"
}

cache_lock_for() {
    local root_path="$1"
    printf '%s/%s.lock' "$(common_cache_lock_dir)" "$(common_hash "$root_path")"
}

cache_exists() { [[ -f "$(cache_file_for "$1")" ]]; }

cache_read() {
    local f
    f=$(cache_file_for "$1")
    [[ -f "$f" ]] || return 1
    cat "$f"
}

cache_write() {
    local root_path="$1" json="$2" f
    f=$(cache_file_for "$root_path")
    mkdir -p "$(dirname "$f")"
    printf '%s' "$json" >"$f"
}

cache_delete() {
    local f
    f=$(cache_file_for "$1")
    [[ -f "$f" ]] && rm -f "$f" || true
}

# Returns 0 if cache exists and is within TTL seconds of now.
cache_is_fresh() {
    local root_path="$1" ttl="$2" f checked age
    f=$(cache_file_for "$root_path")
    [[ -f "$f" ]] || return 1
    checked=$(jq -r '.checked_at // 0' "$f")
    [[ "$checked" =~ ^[0-9]+$ ]] || return 1
    (( ttl == 0 )) && return 1
    age=$(( $(common_now) - checked ))
    (( age < ttl ))
}

# Acquire a per-site lock via mkdir (atomic on both macOS + Linux).
# Echo nothing on success, return 1 if already held.
cache_lock_acquire() {
    local root_path="$1" lock
    lock=$(cache_lock_for "$root_path")
    mkdir "$lock" 2>/dev/null
}

cache_lock_release() {
    local root_path="$1" lock
    lock=$(cache_lock_for "$root_path")
    rmdir "$lock" 2>/dev/null || true
}

cache_is_locked() {
    local lock
    lock=$(cache_lock_for "$1")
    [[ -d "$lock" ]]
}
