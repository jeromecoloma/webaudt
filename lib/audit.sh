#!/usr/bin/env bash
# audit.sh — run composer/npm audit, parse output, drive refresh + status.

AUDIT_ADVISORY_CAP=50

# ---- Pure parsers (stdin = raw audit JSON, stdout = cache ecosystem block) ----

# Composer JSON → normalized ecosystem block.
# Caller supplies audit_path via env: AUDIT_PATH=...
audit_parse_composer() {
    jq --arg audit_path "${AUDIT_PATH:-}" --argjson cap "$AUDIT_ADVISORY_CAP" '
        def norm_sev(s):
            (s // "info" | ascii_downcase) as $x
            | if $x == "medium" then "moderate" else $x end;

        (.advisories // {}) as $adv
        | [ $adv | to_entries[] | .key as $pkg | .value[] | {
                id:       (.cve // .advisoryId // ""),
                package:  $pkg,
                severity: norm_sev(.severity),
                title:    (.title // ""),
                affected: (.affectedVersions // "")
          } ] as $list
        | ($list | group_by(.severity) | map({(.[0].severity): length}) | add // {}) as $by
        | {
            status: "ok",
            audit_path: $audit_path,
            counts: {
                critical: ($by.critical // 0),
                high:     ($by.high // 0),
                moderate: ($by.moderate // 0),
                low:      ($by.low // 0),
                info:     ($by.info // 0)
            },
            advisories: ($list | .[0:$cap])
        }
    '
}

# npm JSON → normalized ecosystem block.
audit_parse_npm() {
    jq --arg audit_path "${AUDIT_PATH:-}" --argjson cap "$AUDIT_ADVISORY_CAP" '
        def norm_sev(s):
            (s // "info" | ascii_downcase) as $x
            | if $x == "medium" then "moderate" else $x end;

        (.metadata.vulnerabilities // {}) as $m
        | (.vulnerabilities // {}) as $v
        | [ $v | to_entries[] | .key as $pkg | .value | {
                id:       ($pkg + "@" + (.range // "")),
                package:  (.name // $pkg),
                severity: norm_sev(.severity),
                title:    ((.via // []) | map(if type == "object" then (.title // .name // "") else . end) | map(tostring) | join("; ")),
                affected: (.range // "")
          } ] as $list
        | {
            status: "ok",
            audit_path: $audit_path,
            counts: {
                critical: ($m.critical // 0),
                high:     ($m.high // 0),
                moderate: ($m.moderate // 0),
                low:      ($m.low // 0),
                info:     ($m.info // 0)
            },
            advisories: ($list | .[0:$cap])
        }
    '
}

# Build an error ecosystem block.
audit_error_block() {
    local audit_path="$1" message="$2"
    jq -n --arg p "$audit_path" --arg m "$message" '{
        status: "error",
        audit_path: $p,
        error: $m
    }'
}

audit_na_block() {
    jq -n '{ status: "not_applicable" }'
}

# ---- Runners ----

# Run composer audit at the given path. Echo cache ecosystem block on stdout.
audit_run_composer() {
    local cpath="$1" composer_bin="$2" hash="$3"
    local stderr="/tmp/webaudt-composer-${hash}.stderr"
    : >"$stderr"

    if ! command -v "$composer_bin" >/dev/null 2>&1; then
        audit_error_block "$cpath" "composer binary not found: $composer_bin"
        return 0
    fi
    if [[ ! -d "$cpath" ]]; then
        audit_error_block "$cpath" "audit path does not exist: $cpath"
        return 0
    fi

    local out
    out=$(cd "$cpath" && "$composer_bin" audit --format=json --no-interaction --locked 2>"$stderr" || true)
    if ! printf '%s' "$out" | jq -e . >/dev/null 2>&1; then
        local err
        err=$(head -c 1000 "$stderr" 2>/dev/null || printf '')
        [[ -z "$err" ]] && err="composer audit produced no parseable JSON"
        audit_error_block "$cpath" "$err"
        return 0
    fi
    AUDIT_PATH="$cpath" printf '%s' "$out" | audit_parse_composer
}

# Run npm audit at the given path. Echo cache ecosystem block on stdout.
audit_run_npm() {
    local npath="$1" npm_bin="$2" hash="$3"
    local stderr="/tmp/webaudt-npm-${hash}.stderr"
    : >"$stderr"

    if ! command -v "$npm_bin" >/dev/null 2>&1; then
        audit_error_block "$npath" "npm binary not found: $npm_bin"
        return 0
    fi
    if [[ ! -d "$npath" ]]; then
        audit_error_block "$npath" "audit path does not exist: $npath"
        return 0
    fi

    local out
    out=$(cd "$npath" && "$npm_bin" audit --json --audit-level=info 2>"$stderr" || true)
    if ! printf '%s' "$out" | jq -e . >/dev/null 2>&1; then
        local err
        err=$(head -c 1000 "$stderr" 2>/dev/null || printf '')
        [[ -z "$err" ]] && err="npm audit produced no parseable JSON"
        audit_error_block "$npath" "$err"
        return 0
    fi
    AUDIT_PATH="$npath" printf '%s' "$out" | audit_parse_npm
}

# Run audits for one site (by config JSON object) and write cache.
audit_run_site() {
    local site_json="$1"
    local name path type composer_path npm_path enabled
    name=$(printf '%s' "$site_json" | jq -r '.name')
    path=$(printf '%s' "$site_json" | jq -r '.path')
    type=$(printf '%s' "$site_json" | jq -r '.type')
    composer_path=$(printf '%s' "$site_json" | jq -r '.composer_path // .path')
    npm_path=$(printf '%s' "$site_json"      | jq -r '.npm_path // .path')
    enabled=$(printf '%s' "$site_json"       | jq -r '.enabled // true')

    if [[ "$enabled" != "true" ]]; then
        return 0
    fi

    local composer_bin npm_bin
    composer_bin=$(config_setting composer_bin composer)
    npm_bin=$(config_setting npm_bin npm)

    local hash start end
    hash=$(common_hash "$path")
    start=$(common_now)

    if ! cache_lock_acquire "$path"; then
        return 0
    fi
    # Release lock on exit of this function.
    trap 'cache_lock_release "'"$path"'"' RETURN

    local composer_block npm_block
    case "$type" in
        composer) composer_block=$(audit_run_composer "$composer_path" "$composer_bin" "$hash"); npm_block=$(audit_na_block) ;;
        npm)      composer_block=$(audit_na_block); npm_block=$(audit_run_npm "$npm_path" "$npm_bin" "$hash") ;;
        both)     composer_block=$(audit_run_composer "$composer_path" "$composer_bin" "$hash"); npm_block=$(audit_run_npm "$npm_path" "$npm_bin" "$hash") ;;
    esac

    end=$(common_now)
    local duration_ms=$(( (end - start) * 1000 ))

    local cache_json
    cache_json=$(jq -n \
        --arg name "$name" \
        --arg path "$path" \
        --argjson checked_at "$end" \
        --argjson duration_ms "$duration_ms" \
        --argjson composer "$composer_block" \
        --argjson npm "$npm_block" \
        '{
            schema_version: 1,
            name: $name,
            path: $path,
            checked_at: $checked_at,
            duration_ms: $duration_ms,
            composer: $composer,
            npm: $npm
        }')

    cache_write "$path" "$cache_json"
}

# ---- Subcommand drivers ----

# Compute the worst severity across an entire cache JSON (both ecosystems).
audit_cache_worst() {
    local cache_json="$1"
    local c n
    for sev in critical high moderate low info; do
        c=$(printf '%s' "$cache_json" | jq -r ".composer.counts.$sev // 0")
        n=$(printf '%s' "$cache_json" | jq -r ".npm.counts.$sev // 0")
        if (( c + n > 0 )); then printf '%s' "$sev"; return; fi
    done
    # Error sticky.
    local cs ns
    cs=$(printf '%s' "$cache_json" | jq -r '.composer.status // "ok"')
    ns=$(printf '%s' "$cache_json" | jq -r '.npm.status // "ok"')
    if [[ "$cs" == "error" || "$ns" == "error" ]]; then printf 'error'; return; fi
    printf 'clean'
}

audit_severity_exit_code() {
    case "$1" in
        critical) return 3 ;;
        high)     return 2 ;;
        moderate|low|info) return 1 ;;
        error)    return 10 ;;
        *)        return 0 ;;
    esac
}

# webaudt refresh [names...] [--all]
audit_refresh() {
    local force=0 names=()
    while (( $# )); do
        case "$1" in
            --all) force=1 ;;
            -*)    common_die "unknown flag: $1" ;;
            *)     names+=("$1") ;;
        esac
        shift
    done

    local parallel ttl
    parallel=$(config_setting parallel_audits 4)
    ttl=$(config_setting cache_ttl 3600)

    local targets=()
    if (( ${#names[@]} > 0 )); then
        local n
        for n in "${names[@]}"; do
            local s
            s=$(config_site_by_name "$n")
            [[ -n "$s" ]] || common_die "no such site: $n"
            targets+=("$s")
        done
        force=1   # named refresh always ignores TTL
    else
        local s
        while IFS= read -r s; do
            [[ -z "$s" ]] && continue
            if (( force )); then
                targets+=("$s")
            else
                local p
                p=$(printf '%s' "$s" | jq -r '.path')
                if ! cache_is_fresh "$p" "$ttl"; then
                    targets+=("$s")
                fi
            fi
        done < <(config_sites)
    fi

    if (( ${#targets[@]} == 0 )); then
        printf 'webaudt: nothing to refresh.\n'
        return 0
    fi

    local s name
    for s in "${targets[@]}"; do
        name=$(printf '%s' "$s" | jq -r '.name')
        while [ "$(jobs -rp | wc -l)" -ge "$parallel" ]; do wait -n; done
        ( audit_run_site "$s" ) &
        printf 'webaudt: refreshing %s\n' "$name"
    done
    wait

    # Compute worst severity across refreshed targets and exit accordingly.
    local worst="clean"
    for s in "${targets[@]}"; do
        local p c w
        p=$(printf '%s' "$s" | jq -r '.path')
        c=$(cache_read "$p" 2>/dev/null || printf '{}')
        w=$(audit_cache_worst "$c")
        case "$w" in
            critical) worst="critical" ;;
            high)     [[ "$worst" != "critical" ]] && worst="high" ;;
            moderate) [[ "$worst" != "critical" && "$worst" != "high" ]] && worst="moderate" ;;
            low|info) [[ "$worst" == "clean" ]] && worst="$w" ;;
            error)    [[ "$worst" == "clean" ]] && worst="error" ;;
        esac
    done
    audit_severity_exit_code "$worst"
}

# Internal: refresh one site by name.
audit_refresh_one() {
    local name="${1:?name required}"
    local s
    s=$(config_site_by_name "$name")
    [[ -n "$s" ]] || common_die "no such site: $name"
    audit_run_site "$s"
}

# webaudt status [name] [--json]
audit_status() {
    local emit_json=0 name=""
    while (( $# )); do
        case "$1" in
            --json) emit_json=1 ;;
            -*)     common_die "unknown flag: $1" ;;
            *)      name="$1" ;;
        esac
        shift
    done

    local worst="clean" sites_json="[]"
    local s
    while IFS= read -r s; do
        [[ -z "$s" ]] && continue
        local sn p c w
        sn=$(printf '%s' "$s" | jq -r '.name')
        [[ -n "$name" && "$sn" != "$name" ]] && continue
        p=$(printf '%s' "$s" | jq -r '.path')
        c=$(cache_read "$p" 2>/dev/null || printf '{}')
        w=$(audit_cache_worst "$c")
        case "$w" in
            critical) worst="critical" ;;
            high)     [[ "$worst" != "critical" ]] && worst="high" ;;
            moderate) [[ "$worst" != "critical" && "$worst" != "high" ]] && worst="moderate" ;;
            low|info) [[ "$worst" == "clean" ]] && worst="$w" ;;
            error)    [[ "$worst" == "clean" ]] && worst="error" ;;
        esac
        if (( emit_json )); then
            sites_json=$(printf '%s' "$sites_json" | jq --argjson c "$c" '. + [$c]')
        else
            local ts="never"
            local checked
            checked=$(printf '%s' "$c" | jq -r '.checked_at // 0')
            [[ "$checked" != "0" ]] && ts=$(common_relative_time "$checked")
            printf '%-20s  %-10s  %s\n' "$sn" "$w" "$ts"
        fi
    done < <(config_sites)

    if (( emit_json )); then
        printf '%s\n' "$sites_json" | jq .
    fi
    audit_severity_exit_code "$worst"
}
