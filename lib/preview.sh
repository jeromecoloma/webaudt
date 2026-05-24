#!/usr/bin/env bash
# preview.sh — render fzf preview pane + full details view.

# Render the right-pane preview for a given site name.
preview_render() {
    local name="${1:?name required}"
    local s
    s=$(config_site_by_name "$name") || true
    if [[ -z "$s" ]]; then
        printf 'unknown site: %s\n' "$name"
        return 0
    fi

    local path type
    path=$(printf '%s' "$s" | jq -r '.path')
    type=$(printf '%s' "$s" | jq -r '.type')

    if common_use_color; then printf '\033[1m%s\033[0m\n' "$name"; else printf '%s\n' "$name"; fi
    printf 'Path: %s\n' "$path"
    printf 'Type: %s\n' "$type"

    if ! cache_exists "$path"; then
        printf '\n(never checked — press r to refresh)\n'
        return 0
    fi

    local c checked
    c=$(cache_read "$path")
    checked=$(printf '%s' "$c" | jq -r '.checked_at // 0')
    if (( checked > 0 )); then
        printf 'Last checked: %s (%s)\n' "$(common_abs_time "$checked")" "$(common_relative_time "$checked")"
    fi

    local eco
    for eco in composer npm; do
        local block status
        block=$(printf '%s' "$c" | jq -c ".$eco // {}")
        status=$(printf '%s' "$block" | jq -r '.status // "not_applicable"')
        [[ "$status" == "not_applicable" ]] && continue

        printf '\n'
        if common_use_color; then printf '\033[1m%s\033[0m\n' "$eco"; else printf '%s\n' "$eco"; fi

        local ap
        ap=$(printf '%s' "$block" | jq -r '.audit_path // ""')
        if [[ -n "$ap" && "$ap" != "$path" ]]; then
            printf '  auditing: %s\n' "$ap"
        fi

        if [[ "$status" == "error" ]]; then
            local err
            err=$(printf '%s' "$block" | jq -r '.error // "unknown error"')
            printf '  ERROR: %s\n' "$err"
            continue
        fi

        local counts summary
        counts=$(printf '%s' "$block" | jq -c '.counts // {}')
        summary=$(preview_counts_summary "$counts")
        printf '  %s\n' "$summary"

        local n_adv adv_count i id sev pkg title affected
        n_adv=$(printf '%s' "$block" | jq -r '(.advisories // []) | length')
        adv_count=$n_adv
        (( adv_count > 10 )) && adv_count=10
        for ((i=0; i<adv_count; i++)); do
            local a
            a=$(printf '%s' "$block" | jq -c ".advisories[$i]")
            id=$(printf '%s' "$a"       | jq -r '.id // ""')
            sev=$(printf '%s' "$a"      | jq -r '.severity // ""')
            pkg=$(printf '%s' "$a"      | jq -r '.package // ""')
            title=$(printf '%s' "$a"    | jq -r '.title // ""')
            affected=$(printf '%s' "$a" | jq -r '.affected // ""')
            printf '   • %s (%s)\n     %s  %s\n' "$id" "$(common_color "$(common_severity_color "$sev")" "$sev")" "$pkg" "$affected"
            [[ -n "$title" ]] && printf '     %s\n' "$title"
        done
        if (( n_adv > 10 )); then
            printf '   … and %d more\n' $(( n_adv - 10 ))
        fi
    done
}

# Render a one-line counts summary like "1C 2H 3M · 0 low" or "clean".
preview_counts_summary() {
    local counts="$1"
    local crit high mod low info
    crit=$(printf '%s' "$counts" | jq -r '.critical // 0')
    high=$(printf '%s' "$counts" | jq -r '.high // 0')
    mod=$(printf '%s'  "$counts" | jq -r '.moderate // 0')
    low=$(printf '%s'  "$counts" | jq -r '.low // 0')
    info=$(printf '%s' "$counts" | jq -r '.info // 0')
    if (( crit + high + mod + low + info == 0 )); then
        printf 'clean'
        return
    fi
    local parts=()
    (( crit > 0 )) && parts+=("$(common_color "$(common_severity_color critical)" "${crit}C")")
    (( high > 0 )) && parts+=("$(common_color "$(common_severity_color high)"     "${high}H")")
    (( mod  > 0 )) && parts+=("$(common_color "$(common_severity_color moderate)" "${mod}M")")
    (( low  > 0 )) && parts+=("$(common_color "$(common_severity_color low)"      "${low}L")")
    (( info > 0 )) && parts+=("${info}I")
    local IFS=' '
    printf '%s' "${parts[*]}"
}

# Full details view — pretty-printed JSON, piped through a pager.
preview_details() {
    local name="${1:?name required}"
    local s
    s=$(config_site_by_name "$name")
    [[ -n "$s" ]] || common_die "no such site: $name"
    local path
    path=$(printf '%s' "$s" | jq -r '.path')
    cache_exists "$path" || { printf 'No cached audit yet for %s.\n' "$name"; return 0; }

    local cache
    cache=$(cache_read "$path")
    if command -v bat >/dev/null 2>&1; then
        printf '%s' "$cache" | jq . | bat --paging=always -l json --style=plain
    else
        printf '%s' "$cache" | jq . | ${PAGER:-less -R}
    fi
}
