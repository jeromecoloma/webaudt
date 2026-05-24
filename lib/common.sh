#!/usr/bin/env bash
# common.sh — shared helpers: XDG paths, hashing, time, color/emoji.

set -euo pipefail
IFS=$'\n\t'

if [[ -z "${BASH_VERSINFO:-}" || "${BASH_VERSINFO[0]}" -lt 4 ]]; then
    printf 'webaudt: bash 4+ required (current: %s)\n' "${BASH_VERSION:-unknown}" >&2
    printf 'On macOS: brew install bash\n' >&2
    exit 1
fi

WEBAUDT_VERSION="0.1.0"

common_xdg_config() {
    printf '%s/webaudt' "${XDG_CONFIG_HOME:-$HOME/.config}"
}

common_xdg_cache() {
    printf '%s/webaudt' "${XDG_CACHE_HOME:-$HOME/.cache}"
}

common_config_file() {
    printf '%s/config.toml' "$(common_xdg_config)"
}

common_cache_sites_dir() {
    printf '%s/sites' "$(common_xdg_cache)"
}

common_cache_lock_dir() {
    printf '%s/lock' "$(common_xdg_cache)"
}

common_ensure_dirs() {
    mkdir -p "$(common_xdg_config)" "$(common_cache_sites_dir)" "$(common_cache_lock_dir)"
}

common_hash() {
    local path="$1"
    printf '%s' "$path" | shasum -a 1 | cut -c1-8
}

common_now() { date +%s; }

common_relative_time() {
    local then="$1" now diff
    now=$(common_now)
    diff=$(( now - then ))
    if (( diff < 60 )); then printf '%ds ago' "$diff"
    elif (( diff < 3600 )); then printf '%dm ago' $((diff/60))
    elif (( diff < 86400 )); then printf '%dh ago' $((diff/3600))
    else date -r "$then" +'%Y-%m-%d' 2>/dev/null || date -d "@$then" +'%Y-%m-%d'
    fi
}

common_abs_time() {
    local epoch="$1"
    date -r "$epoch" +'%Y-%m-%d %H:%M:%S' 2>/dev/null || date -d "@$epoch" +'%Y-%m-%d %H:%M:%S'
}

# Color handling. Respect NO_COLOR and settings.color (set via WEBAUDT_COLOR env).
common_use_color() {
    [[ -n "${NO_COLOR:-}" ]] && return 1
    case "${WEBAUDT_COLOR:-auto}" in
        never) return 1 ;;
        always) return 0 ;;
        auto|*) [[ -t 1 ]] ;;
    esac
}

common_color() {
    local code="$1"; shift
    if common_use_color; then
        printf '\033[%sm%s\033[0m' "$code" "$*"
    else
        printf '%s' "$*"
    fi
}

common_use_emoji() {
    [[ -z "${AUDT_NO_EMOJI:-}" && -z "${WEBAUDT_NO_EMOJI:-}" ]]
}

# Map severity bucket to icon (emoji or ASCII fallback).
common_status_icon() {
    local status="$1"
    if common_use_emoji; then
        case "$status" in
            critical) printf '🔴' ;;
            high)     printf '🟠' ;;
            moderate) printf '🟡' ;;
            low|info) printf '🔵' ;;
            clean)    printf '🟢' ;;
            never)    printf '⚪' ;;
            running)  printf '⏳' ;;
            error)    printf '⚠' ;;
            disabled) printf '🚫' ;;
            *)        printf '?' ;;
        esac
    else
        case "$status" in
            critical) printf '!' ;;
            high)     printf 'H' ;;
            moderate) printf 'M' ;;
            low|info) printf 'L' ;;
            clean)    printf '.' ;;
            never)    printf '?' ;;
            running)  printf '~' ;;
            error)    printf 'x' ;;
            disabled) printf '-' ;;
            *)        printf '?' ;;
        esac
    fi
}

common_severity_color() {
    case "$1" in
        critical) printf '31' ;;       # red
        high)     printf '38;5;208' ;; # orange
        moderate) printf '33' ;;       # yellow
        low|info) printf '34' ;;       # blue
        clean)    printf '32' ;;       # green
        *)        printf '37' ;;       # white
    esac
}

common_die() {
    printf 'webaudt: %s\n' "$*" >&2
    exit 1
}

common_warn() {
    printf 'webaudt: %s\n' "$*" >&2
}

# Worst severity across a counts JSON object {critical:N, high:N, moderate:N, low:N, info:N}.
# Echoes one of: critical|high|moderate|low|info|clean
common_worst_severity() {
    local counts="$1" sev
    for sev in critical high moderate low info; do
        local n
        n=$(printf '%s' "$counts" | jq -r --arg s "$sev" '.[$s] // 0')
        if (( n > 0 )); then
            printf '%s' "$sev"
            return
        fi
    done
    printf 'clean'
}
