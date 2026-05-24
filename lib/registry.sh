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

# Render a styled "already registered" error block.
registry_print_duplicate_error() {
    local existing="$1" path="$2"
    local cfg
    cfg=$(common_config_file)

    printf '\n'
    if command -v gum >/dev/null 2>&1 && common_use_color; then
        gum style \
            --border rounded \
            --border-foreground 208 \
            --padding "1 2" \
            --margin "0 0 1 0" \
            "$(gum style --foreground 208 --bold '⚠  site already registered')" \
            "" \
            "$(printf '  name:  %s' "$(gum style --foreground 51 "$existing")")" \
            "$(printf '  path:  %s' "$path")"
        printf '  next:\n'
        printf '    %s  %s\n' "$(common_color 244 'remove:')" "$(common_color '1;36' "webaudt rm $existing")"
        printf '    %s  %s\n' "$(common_color 244 'or edit:')" "$cfg"
        printf '\n'
    else
        printf '  ⚠  site already registered\n\n'
        printf '    name: %s\n'  "$existing"
        printf '    path: %s\n\n' "$path"
        printf '  next:\n'
        printf '    remove:   webaudt rm %s\n' "$existing"
        printf '    or edit:  %s\n\n' "$cfg"
    fi
}

# Probe for a project-local composer binary, falling back to PATH.
# Search order, first hit wins:
#   <path>/composer.phar, <path>/bin/composer, <path>/vendor/bin/composer,
#   <path>/composer, then `which composer`.
registry_detect_composer_bin() {
    local path="$1" cand
    for cand in composer.phar bin/composer vendor/bin/composer composer; do
        if [[ -x "$path/$cand" ]]; then
            printf '%s' "$path/$cand"
            return
        fi
    done
    command -v composer 2>/dev/null || true
}

# Probe for a project-local npm binary, falling back to PATH.
# Search order: <path>/node_modules/.bin/npm, then `which npm`. If the site has
# a .nvmrc, that's surfaced separately in the confirmation block.
registry_detect_npm_bin() {
    local path="$1"
    if [[ -x "$path/node_modules/.bin/npm" ]]; then
        printf '%s' "$path/node_modules/.bin/npm"
        return
    fi
    command -v npm 2>/dev/null || true
}

# Resolve a unique name by appending -2, -3, ... if needed.
registry_unique_name() {
    local base="$1"
    local candidate="$base"
    local n=2
    while config_site_by_name "$candidate" | grep -q .; do
        candidate="${base}-${n}"
        n=$((n+1))
    done
    printf '%s' "$candidate"
}

registry_add() {
    local raw_path="" name="" type="" composer_path="" npm_path="" composer_bin="" npm_bin=""
    while (( $# )); do
        case "$1" in
            --name)           name="$2"; shift 2 ;;
            --type)           type="$2"; shift 2 ;;
            --composer-path)  composer_path="$2"; shift 2 ;;
            --npm-path)       npm_path="$2"; shift 2 ;;
            --composer-bin)   composer_bin="$2"; shift 2 ;;
            --npm-bin)        npm_bin="$2"; shift 2 ;;
            -*)               common_die "unknown flag: $1" ;;
            *)                [[ -z "$raw_path" ]] || common_die "unexpected arg: $1"; raw_path="$1"; shift ;;
        esac
    done

    [[ -n "$raw_path" ]] || common_die "usage: webaudt add <path> [--name N] [--type T] [--composer-path P] [--npm-path P]"

    local abs
    abs=$(cd "$raw_path" 2>/dev/null && pwd) || common_die "path does not exist or is not a directory: $raw_path"

    # Refuse duplicate path registration.
    local existing
    existing=$(config_get | jq -r --arg p "$abs" '(.sites // [])[] | select(.path == $p) | .name' | head -n1)
    if [[ -n "$existing" ]]; then
        registry_print_duplicate_error "$existing" "$abs"
        exit 1
    fi

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

    # Binary detection (only for ecosystems this site uses).
    local default_composer default_npm
    default_composer=$(config_setting composer_bin composer)
    default_npm=$(config_setting npm_bin npm)

    if [[ "$type" == composer || "$type" == both ]]; then
        if [[ -z "$composer_bin" ]]; then
            composer_bin=$(registry_detect_composer_bin "$composer_path")
            [[ -z "$composer_bin" ]] && composer_bin="$default_composer"
        fi
    fi
    if [[ "$type" == npm || "$type" == both ]]; then
        if [[ -z "$npm_bin" ]]; then
            npm_bin=$(registry_detect_npm_bin "$npm_path")
            [[ -z "$npm_bin" ]] && npm_bin="$default_npm"
        fi
    fi

    # Optional .nvmrc heads-up.
    local nvmrc_note=""
    if [[ "$type" == npm || "$type" == both ]] && [[ -f "$npm_path/.nvmrc" ]]; then
        nvmrc_note=$(<"$npm_path/.nvmrc")
        nvmrc_note="$(printf '%s' "$nvmrc_note" | tr -d '[:space:]')"
    fi

    # Confirmation block.
    local cpath_disp="${composer_path:-—}" npath_disp="${npm_path:-—}"
    printf '\n'
    common_heading "register site"
    printf '\n'
    printf '  %s  %s\n' "$(common_color '1;37' 'name:          ')" "$(common_color 51  "$name")"
    printf '  %s  %s\n' "$(common_color '1;37' 'path:          ')" "$abs"
    printf '  %s  %s\n' "$(common_color '1;37' 'type:          ')" "$(common_color 33  "$type")"
    if [[ "$composer_path" == "$abs" && "$npm_path" == "$abs" ]]; then
        printf '  %s  %s\n' "$(common_color '1;37' 'audit path:    ')" "$abs"
    else
        [[ "$type" == composer || "$type" == both ]] && printf '  %s  %s\n' "$(common_color '1;37' 'composer audit:')" "$cpath_disp"
        [[ "$type" == npm      || "$type" == both ]] && printf '  %s  %s\n' "$(common_color '1;37' 'npm audit:     ')" "$npath_disp"
    fi
    if [[ "$type" == composer || "$type" == both ]]; then
        printf '  %s  %s\n' "$(common_color '1;37' 'composer bin:  ')" "${composer_bin:-(not found)}"
    fi
    if [[ "$type" == npm || "$type" == both ]]; then
        printf '  %s  %s\n' "$(common_color '1;37' 'npm bin:       ')" "${npm_bin:-(not found)}"
        [[ -n "$nvmrc_note" ]] && printf '  %s  %s\n' "$(common_color '1;37' '.nvmrc:        ')" "$nvmrc_note (set via nvm before refreshing)"
    fi
    printf '\n  %s\n\n' "$(common_color 244 "↳ edit any of these later in: $(common_config_file)")"

    if command -v gum >/dev/null 2>&1; then
        gum confirm "Continue?" || { printf 'Cancelled.\n'; return 1; }
    else
        read -r -p "Continue? [y/N] " ans
        [[ "$ans" =~ ^[yY] ]] || { printf 'Cancelled.\n'; return 1; }
    fi

    # Build new site object — only emit fields that differ from defaults.
    local site_obj
    site_obj=$(jq -n \
        --arg name "$name" --arg path "$abs" --arg type "$type" \
        --arg cp "$composer_path" --arg np "$npm_path" \
        --arg cb "$composer_bin" --arg nb "$npm_bin" \
        --arg dcb "$default_composer" --arg dnb "$default_npm" \
        '{ name: $name, path: $path, type: $type }
         + (if ($cp != "" and $cp != $path) then { composer_path: $cp } else {} end)
         + (if ($np != "" and $np != $path) then { npm_path: $np } else {} end)
         + (if ($cb != "" and $cb != $dcb) then { composer_bin: $cb } else {} end)
         + (if ($nb != "" and $nb != $dnb) then { npm_bin: $nb } else {} end)')

    # Append to config.
    local current new
    current=$(config_get)
    new=$(printf '%s' "$current" | jq --argjson s "$site_obj" '.sites = ((.sites // []) + [$s])')
    printf '%s' "$new" | config_write_json

    printf '  %s registered %s\n' "$(common_color 32 '✓')" "$(common_color '1;36' "$name")"
    printf '    edit later: %s\n\n' "$(common_color 244 "$(common_config_file)")"
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
    printf '  %s removed %s\n\n' "$(common_color 32 '✓')" "$(common_color '1;36' "$name")"
}

registry_list() {
    local s name type path ttl
    ttl=$(config_setting cache_ttl 3600)

    local n
    n=$(config_get | jq '(.sites // []) | length')
    if (( n == 0 )); then
        common_heading "registered sites"
        printf '\n  (none yet — try: webaudt add /path/to/site)\n\n'
        return 0
    fi

    common_heading "registered sites ($n)"
    printf '\n'

    while IFS= read -r s; do
        [[ -z "$s" ]] && continue
        name=$(printf '%s' "$s" | jq -r '.name')
        type=$(printf '%s' "$s" | jq -r '.type')
        path=$(printf '%s' "$s" | jq -r '.path')
        local last="never" worst="never" icon
        if cache_exists "$path"; then
            local c checked
            c=$(cache_read "$path")
            checked=$(printf '%s' "$c" | jq -r '.checked_at // 0')
            (( checked > 0 )) && last=$(common_relative_time "$checked")
            worst=$(audit_cache_worst "$c")
        fi
        icon=$(common_status_icon "$worst")
        local status_line type_col type_label
        case "$type" in
            both)     type_label="composer+npm" ;;
            *)        type_label="$type" ;;
        esac
        type_col=$(common_color 244 "$type_label")
        if [[ "$worst" == "never" ]]; then
            status_line=$(common_color 244 "never checked")
        else
            status_line=$(printf '%s · %s' \
                "$(common_color "$(common_severity_color "$worst")" "$worst")" \
                "$(common_color 244 "$last")")
        fi

        printf '  %s  %s  %s  %s\n' \
            "$icon" \
            "$(common_color '1;36' "$name")" \
            "$type_col" \
            "$status_line"
        printf '      %s\n\n' "$(common_color 244 "$path")"
    done < <(config_sites)
}
