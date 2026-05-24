#!/usr/bin/env bash
# registry.sh — add / remove / list registered sites.

# Detect ecosystem path for one of {composer,package} starting at <base>.
# Echoes the resolved directory (base or base/www), or empty if not found.
registry_detect_path() {
    local base="$1" manifest="$2"
    if [[ -f "$base/$manifest" ]]; then
        printf '%s' "$base"
    elif [[ -f "$base/www/$manifest" ]]; then
        printf '%s/www' "$base"
    fi
}

# Resolve a unique name by appending -2, -3, ... if needed.
registry_unique_name() {
    local base="$1" candidate="$base" n=2
    while config_site_by_name "$candidate" | grep -q .; do
        candidate="${base}-${n}"
        n=$((n+1))
    done
    printf '%s' "$candidate"
}

registry_add() {
    local raw_path="" name="" type="" composer_path="" npm_path=""
    while (( $# )); do
        case "$1" in
            --name)           name="$2"; shift 2 ;;
            --type)           type="$2"; shift 2 ;;
            --composer-path)  composer_path="$2"; shift 2 ;;
            --npm-path)       npm_path="$2"; shift 2 ;;
            -*)               common_die "unknown flag: $1" ;;
            *)                [[ -z "$raw_path" ]] || common_die "unexpected arg: $1"; raw_path="$1"; shift ;;
        esac
    done

    [[ -n "$raw_path" ]] || common_die "usage: webaudt add <path> [--name N] [--type T] [--composer-path P] [--npm-path P]"

    local abs
    abs=$(cd "$raw_path" 2>/dev/null && pwd) || common_die "path does not exist or is not a directory: $raw_path"

    # Resolve per-ecosystem paths.
    if [[ -z "$composer_path" ]]; then
        composer_path=$(registry_detect_path "$abs" "composer.json")
    else
        composer_path=$(cd "$composer_path" 2>/dev/null && pwd) || common_die "--composer-path does not exist: $composer_path"
    fi
    if [[ -z "$npm_path" ]]; then
        npm_path=$(registry_detect_path "$abs" "package.json")
    else
        npm_path=$(cd "$npm_path" 2>/dev/null && pwd) || common_die "--npm-path does not exist: $npm_path"
    fi

    # Auto-derive type if not given.
    if [[ -z "$type" ]]; then
        if [[ -n "$composer_path" && -n "$npm_path" ]]; then type="both"
        elif [[ -n "$composer_path" ]]; then type="composer"
        elif [[ -n "$npm_path" ]]; then type="npm"
        else common_die "no composer.json or package.json found in $abs or $abs/www; pass --type and --composer-path / --npm-path explicitly to register anyway"
        fi
    else
        case "$type" in
            composer) [[ -n "$composer_path" ]] || common_die "--type composer but no composer.json detected; pass --composer-path" ;;
            npm)      [[ -n "$npm_path"      ]] || common_die "--type npm but no package.json detected; pass --npm-path" ;;
            both)     [[ -n "$composer_path" && -n "$npm_path" ]] || common_die "--type both requires both manifests; pass --composer-path and --npm-path" ;;
            *)        common_die "--type must be composer|npm|both" ;;
        esac
    fi

    # Name.
    [[ -n "$name" ]] || name=$(basename "$abs")
    name=$(registry_unique_name "$name")

    # Confirmation block.
    local cpath_disp="${composer_path:-—}" npath_disp="${npm_path:-—}"
    printf 'Register site:\n'
    printf '  name:           %s\n' "$name"
    printf '  path:           %s\n' "$abs"
    printf '  type:           %s\n' "$type"
    if [[ "$composer_path" == "$abs" && "$npm_path" == "$abs" ]]; then
        printf '  audit path:     %s\n' "$abs"
    else
        [[ "$type" == composer || "$type" == both ]] && printf '  composer audit: %s\n' "$cpath_disp"
        [[ "$type" == npm      || "$type" == both ]] && printf '  npm audit:      %s\n' "$npath_disp"
    fi
    printf '\n'

    if command -v gum >/dev/null 2>&1; then
        gum confirm "Continue?" || { printf 'Cancelled.\n'; return 1; }
    else
        read -r -p "Continue? [y/N] " ans
        [[ "$ans" =~ ^[yY] ]] || { printf 'Cancelled.\n'; return 1; }
    fi

    # Build new site object — only emit composer_path / npm_path when they differ from path.
    local site_obj
    site_obj=$(jq -n \
        --arg name "$name" --arg path "$abs" --arg type "$type" \
        --arg cp "$composer_path" --arg np "$npm_path" \
        '{ name: $name, path: $path, type: $type }
         + (if ($cp != "" and $cp != $path) then { composer_path: $cp } else {} end)
         + (if ($np != "" and $np != $path) then { npm_path: $np } else {} end)')

    # Append to config.
    local current new
    current=$(config_get)
    new=$(printf '%s' "$current" | jq --argjson s "$site_obj" '.sites = ((.sites // []) + [$s])')
    printf '%s' "$new" | config_write_json

    printf 'Registered: %s\n' "$name"
}

registry_rm() {
    local name="${1:?usage: webaudt rm <name>}"
    local s
    s=$(config_site_by_name "$name")
    [[ -n "$s" ]] || common_die "no such site: $name"

    if command -v gum >/dev/null 2>&1; then
        gum confirm "Remove site '$name' and its cached audit?" || { printf 'Cancelled.\n'; return 1; }
    else
        read -r -p "Remove site '$name' and its cached audit? [y/N] " ans
        [[ "$ans" =~ ^[yY] ]] || { printf 'Cancelled.\n'; return 1; }
    fi

    local path current new
    path=$(printf '%s' "$s" | jq -r '.path')
    current=$(config_get)
    new=$(printf '%s' "$current" | jq --arg n "$name" '.sites = ((.sites // []) | map(select(.name != $n)))')
    printf '%s' "$new" | config_write_json
    cache_delete "$path"
    printf 'Removed: %s\n' "$name"
}

registry_list() {
    local s name type path ttl
    ttl=$(config_setting cache_ttl 3600)
    printf '%-20s  %-10s  %-12s  %s\n' "NAME" "TYPE" "LAST" "PATH"
    while IFS= read -r s; do
        [[ -z "$s" ]] && continue
        name=$(printf '%s' "$s" | jq -r '.name')
        type=$(printf '%s' "$s" | jq -r '.type')
        path=$(printf '%s' "$s" | jq -r '.path')
        local last="never" worst="-"
        if cache_exists "$path"; then
            local c checked
            c=$(cache_read "$path")
            checked=$(printf '%s' "$c" | jq -r '.checked_at // 0')
            (( checked > 0 )) && last=$(common_relative_time "$checked")
            worst=$(audit_cache_worst "$c")
        fi
        printf '%-20s  %-10s  %-12s  %s  [%s]\n' "$name" "$type" "$last" "$path" "$worst"
    done < <(config_sites)
}
