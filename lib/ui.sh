#!/usr/bin/env bash
# ui.sh — fzf-driven sidebar.

# Emit one sidebar line per registered site. Tab-separated:
#   icon \t name \t summary \t relative_time
ui_emit_list() {
    local s
    while IFS= read -r s; do
        [[ -z "$s" ]] && continue
        ui_format_line "$s"
    done < <(config_sites)
}

ui_format_line() {
    local s="$1"
    local name path type enabled
    name=$(printf '%s' "$s" | jq -r '.name')
    path=$(printf '%s' "$s" | jq -r '.path')
    type=$(printf '%s' "$s" | jq -r '.type')
    enabled=$(printf '%s' "$s" | jq -r '.enabled // true')

    local icon summary when

    if [[ "$enabled" != "true" ]]; then
        icon=$(common_status_icon disabled)
        summary="disabled"
        when=""
    elif cache_is_locked "$path"; then
        icon=$(common_status_icon running)
        summary="auditing…"
        when=""
    elif ! cache_exists "$path"; then
        icon=$(common_status_icon never)
        summary="(never checked)"
        when=""
    else
        local c worst checked
        c=$(cache_read "$path")
        worst=$(audit_cache_worst "$c")
        icon=$(common_status_icon "$worst")
        summary=$(ui_summary_for_cache "$c" "$type")
        checked=$(printf '%s' "$c" | jq -r '.checked_at // 0')
        if (( checked > 0 )); then when=$(common_relative_time "$checked"); else when=""; fi
    fi

    printf '%s\t%s\t%s\t%s\n' "$icon" "$name" "$summary" "$when"
}

# "composer:1C 2H · npm:clean" — only non-zero categories, "clean" when empty.
ui_summary_for_cache() {
    local c="$1" type="$2"
    local parts=() eco
    for eco in composer npm; do
        local status block
        block=$(printf '%s' "$c" | jq -c ".$eco // {}")
        status=$(printf '%s' "$block" | jq -r '.status // "not_applicable"')
        case "$status" in
            not_applicable) continue ;;
            error)          parts+=("$eco:error") ;;
            ok)
                local counts s
                counts=$(printf '%s' "$block" | jq -c '.counts // {}')
                s=$(preview_counts_summary "$counts")
                # Strip ANSI for summary line so fzf width math stays sane.
                s=$(printf '%s' "$s" | sed -E 's/\x1b\[[0-9;]*m//g')
                parts+=("$eco:$s")
                ;;
        esac
    done
    local IFS=' · '
    printf '%s' "${parts[*]}"
}

# Open the TUI.
ui_open() {
    # Validate config / dependencies up front.
    config_get >/dev/null

    local sites_n
    sites_n=$(config_get | jq '(.sites // []) | length')

    if (( sites_n == 0 )); then
        common_banner
        printf '\n'
        if command -v gum >/dev/null 2>&1 && common_use_color; then
            gum style --foreground 244 --padding "0 2" \
                "no sites registered yet." \
                "" \
                "get started:" \
                "  $(gum style --foreground 51 'webaudt add /path/to/site')"
        else
            printf '  no sites registered yet.\n\n  get started:\n    webaudt add /path/to/site\n'
        fi
        printf '\n'
        return 0
    fi

    if ! command -v fzf >/dev/null 2>&1; then
        common_die "fzf not found; install it or run 'webaudt doctor'"
    fi

    # Kick off background refresh for any stale sites.
    local ttl
    ttl=$(config_setting cache_ttl 3600)
    local s p
    while IFS= read -r s; do
        [[ -z "$s" ]] && continue
        p=$(printf '%s' "$s" | jq -r '.path')
        if ! cache_is_fresh "$p" "$ttl"; then
            ( audit_run_site "$s" ) >/dev/null 2>&1 &
        fi
    done < <(config_sites)
    disown -a 2>/dev/null || true

    # The wrapper script path so fzf bindings can call us.
    local self="$WEBAUDT_ROOT/bin/webaudt"

    ui_emit_list | fzf --ansi --reverse \
        --delimiter=$'\t' \
        --with-nth=1,2,3,4 \
        --preview "$self _preview {2}" \
        --preview-window=right:60%:wrap \
        --bind "r:execute-silent($self _refresh_one {2})+reload($self _list)" \
        --bind "R:execute($self refresh --all)+reload($self _list)" \
        --bind "enter:execute($self _details {2})" \
        --color "border:39,header:51,prompt:51,pointer:208,marker:46,info:244" \
        --header $'  webaudt · r refresh · R refresh all · enter details · esc quit\n'
}
