#!/usr/bin/env bash
# doctor.sh — verify required dependencies and versions.

# Compare two dotted versions. Returns 0 if $1 >= $2.
doctor_ver_ge() {
    local have="$1" want="$2"
    [[ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n1)" == "$want" ]]
}

# Probe one tool. Args: name, min_version, version_extractor (shell).
# Echoes one line: STATUS\tNAME\tDETAIL
doctor_check() {
    local name="$1" min="$2" extract="$3"
    if ! command -v "$name" >/dev/null 2>&1; then
        printf 'MISSING\t%s\t(min %s)\n' "$name" "$min"
        return 1
    fi
    local v
    v=$(eval "$extract" 2>/dev/null || printf '')
    if [[ -z "$v" ]]; then
        printf 'UNKNOWN\t%s\tinstalled, version unparseable (min %s)\n' "$name" "$min"
        return 0
    fi
    if doctor_ver_ge "$v" "$min"; then
        printf 'OK\t%s\t%s (>= %s)\n' "$name" "$v" "$min"
    else
        printf 'OUTDATED\t%s\t%s (need %s)\n' "$name" "$v" "$min"
        return 1
    fi
}

doctor_print_line() {
    local line="$1"
    local status="${line%%$'\t'*}"
    local rest="${line#*$'\t'}"
    local name="${rest%%$'\t'*}"
    local detail="${rest#*$'\t'}"
    local mark color
    case "$status" in
        OK)       mark="✓"; color="32" ;;
        MISSING)  mark="✗"; color="31" ;;
        OUTDATED) mark="!"; color="33" ;;
        UNKNOWN)  mark="?"; color="33" ;;
        *)        mark="·"; color="37" ;;
    esac
    printf '  %s  %-10s  %s\n' "$(common_color "$color" "$mark")" "$name" "$detail"
}

doctor_install_hints() {
    cat <<'EOF'

Install hints:
  macOS:           brew install bash fzf gum jq yq git
  Debian/Ubuntu:   sudo apt install bash fzf jq git
                   (gum: github.com/charmbracelet/gum; yq: github.com/mikefarah/yq v4)
  Arch:            sudo pacman -S bash fzf gum jq go-yq git
EOF
}

doctor_run() {
    common_banner
    printf '\n'
    common_heading "dependency check"
    printf '\n'
    local rc=0
    local results=()

    results+=("$(doctor_check bash 4.0 'printf "%s" "${BASH_VERSINFO[0]}.${BASH_VERSINFO[1]}"' || true)")
    results+=("$(doctor_check fzf  0.40 'fzf --version | awk "{print \$1}"' || true)")
    results+=("$(doctor_check gum  0.13 'gum --version 2>&1 | grep -oE "[0-9]+\.[0-9]+(\.[0-9]+)?" | head -n1' || true)")
    results+=("$(doctor_check jq   1.6  'jq --version | sed "s/^jq-//"' || true)")
    # Mike Farah's yq prints "yq (https://github.com/mikefarah/yq/) version v4.x.y".
    results+=("$(doctor_check yq   4.0  'yq --version 2>&1 | grep -oE "v?[0-9]+\.[0-9]+(\.[0-9]+)?" | tail -n1 | sed "s/^v//"' || true)")
    results+=("$(doctor_check git  2.20 'git --version | awk "{print \$3}"' || true)")

    # Optional tools — informational only.
    local opt_results=()
    opt_results+=("$(doctor_check composer 2.0 'composer --version 2>/dev/null | grep -oE "[0-9]+\.[0-9]+\.[0-9]+" | head -n1' || true)")
    opt_results+=("$(doctor_check npm      7.0 'npm --version' || true)")

    local r
    for r in "${results[@]}"; do
        doctor_print_line "$r"
        [[ "${r%%$'\t'*}" == "OK" ]] || rc=1
    done

    printf '\n'
    common_heading "optional (only needed for sites of that type)"
    printf '\n'
    for r in "${opt_results[@]}"; do
        doctor_print_line "$r"
    done

    # yq Go vs Python detection.
    if command -v yq >/dev/null 2>&1; then
        if ! yq --version 2>&1 | grep -qi mikefarah; then
            printf '\nWARNING: detected yq does not appear to be the Mike Farah (Go) build.\n'
            printf 'The Python yq is not compatible. See: https://github.com/mikefarah/yq\n'
            rc=1
        fi
    fi

    printf '\n'
    if (( rc != 0 )); then
        doctor_install_hints
        printf '\n  %s one or more dependencies are missing or outdated.\n' "$(common_color 31 '✗')" >&2
    else
        printf '  %s all required dependencies OK.\n' "$(common_color 32 '✓')"
    fi
    return $rc
}
