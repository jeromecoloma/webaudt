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
    if command -v sha1sum >/dev/null 2>&1; then
        printf '%s' "$path" | sha1sum | cut -c1-8
    elif command -v shasum >/dev/null 2>&1; then
        printf '%s' "$path" | shasum -a 1 | cut -c1-8
    else
        common_die "no sha1 tool found (need sha1sum or shasum)"
    fi
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
# IMPORTANT: tty detection is done ONCE at source-time. Doing it inside
# common_use_color would fail for callers using $(common_color ...) since
# command substitution captures stdout into a pipe.
if [[ -n "${NO_COLOR:-}" ]]; then
    __WEBAUDT_TTY=0
elif [[ -t 1 ]]; then
    __WEBAUDT_TTY=1
else
    __WEBAUDT_TTY=0
fi
export __WEBAUDT_TTY

common_use_color() {
    [[ -n "${NO_COLOR:-}" ]] && return 1
    case "${WEBAUDT_COLOR:-auto}" in
        never) return 1 ;;
        always) return 0 ;;
        auto|*) [[ "$__WEBAUDT_TTY" == "1" ]] ;;
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

# Map severity bucket to a small colored glyph. Single-cell Unicode dots so
# columns align cleanly. Opt into chunky emoji via WEBAUDT_EMOJI_ICONS=1.
common_status_icon() {
    local status="$1"
    if [[ -n "${WEBAUDT_EMOJI_ICONS:-}" ]] && common_use_emoji; then
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
        return
    fi

    # ASCII-only mode (no color, no Unicode).
    if [[ -n "${AUDT_NO_EMOJI:-}" || -n "${WEBAUDT_NO_EMOJI:-}" ]]; then
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
        return
    fi

    # Default: small colored Unicode glyph.
    local glyph color
    case "$status" in
        critical) glyph='●'; color=$(common_severity_color critical) ;;
        high)     glyph='●'; color=$(common_severity_color high) ;;
        unknown)  glyph='◆'; color=$(common_severity_color unknown) ;;
        moderate) glyph='●'; color=$(common_severity_color moderate) ;;
        low|info) glyph='●'; color=$(common_severity_color low) ;;
        clean)    glyph='●'; color=$(common_severity_color clean) ;;
        never)    glyph='○'; color='244' ;;
        running)  glyph='◐'; color='39' ;;
        error)    glyph='▲'; color=$(common_severity_color high) ;;
        disabled) glyph='◌'; color='244' ;;
        *)        glyph='·'; color='37' ;;
    esac
    common_color "$color" "$glyph"
}

common_severity_color() {
    case "$1" in
        critical) printf '31' ;;       # red
        high)     printf '38;5;208' ;; # orange
        unknown)  printf '38;5;213' ;; # pink/magenta — unrated, needs review
        moderate) printf '33' ;;       # yellow
        low|info) printf '34' ;;       # blue
        clean)    printf '32' ;;       # green
        *)        printf '37' ;;       # white
    esac
}

# Pretty banner. Uses gum style when available, plain ANSI otherwise.
common_banner() {
    local mode="${1:-full}"   # full | mini
    local art="\
░█░█░█▀▀░█▀▄░█▀█░█░█░█▀▄░▀█▀
░█▄█░█▀▀░█▀▄░█▀█░█░█░█░█░░█░
░▀░▀░▀▀▀░▀▀░░▀░▀░▀▀▀░▀▀░░░▀░"
    local tagline="composer + npm audit monitor"
    local version="v${WEBAUDT_VERSION}"

    if command -v gum >/dev/null 2>&1 && common_use_color; then
        gum style \
            --border rounded \
            --border-foreground 39 \
            --foreground 51 \
            --padding "1 3" \
            --margin "0" \
            --align center \
            "$art" "" "$(printf '%s   %s' "$tagline" "$version")"
    else
        printf '\n%s\n\n  %s   %s\n\n' "$art" "$tagline" "$version"
    fi
}

# Compact one-line section heading.
common_heading() {
    local text="$1"
    if command -v gum >/dev/null 2>&1 && common_use_color; then
        gum style --foreground 51 --bold "▸ $text"
    else
        common_color '1;36' "▸ $text"
        printf '\n'
    fi
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
