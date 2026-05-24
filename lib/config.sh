#!/usr/bin/env bash
# config.sh — load and validate ~/.config/webaudt/config.toml.

# Default values, mirrored from PRD §5.
config_default_cache_ttl=3600
config_default_parallel=4
config_default_composer_bin="composer"
config_default_npm_bin="npm"
config_default_color="auto"

# Write a fresh default config file at the given path.
config_write_default() {
    local path="$1"
    mkdir -p "$(dirname "$path")"
    cat >"$path" <<EOF
# webaudt configuration. See \`webaudt --help\` and the PRD for full schema.

[settings]
cache_ttl = $config_default_cache_ttl
parallel_audits = $config_default_parallel
composer_bin = "$config_default_composer_bin"
npm_bin = "$config_default_npm_bin"
color = "$config_default_color"

# Register sites with: webaudt add /path/to/site
EOF
}

# Load the config file. If it doesn't exist, create a default one.
# Echoes the file contents as JSON on stdout.
config_load_json() {
    local file
    file="$(common_config_file)"
    if [[ ! -f "$file" ]]; then
        config_write_default "$file"
    fi
    yq -p toml -o json '.' "$file"
}

# Validate a config JSON blob on stdin. Exit non-zero on failure.
config_validate() {
    local json
    json=$(cat)

    # settings
    local cache_ttl parallel color
    cache_ttl=$(printf '%s' "$json" | jq -r '.settings.cache_ttl // 3600')
    parallel=$(printf '%s' "$json"  | jq -r '.settings.parallel_audits // 4')
    color=$(printf '%s' "$json"     | jq -r '.settings.color // "auto"')

    [[ "$cache_ttl" =~ ^[0-9]+$ ]] || common_die "settings.cache_ttl must be a non-negative integer, got: $cache_ttl"
    [[ "$parallel"  =~ ^[0-9]+$ ]] || common_die "settings.parallel_audits must be an integer, got: $parallel"
    (( parallel >= 1 && parallel <= 16 )) || common_die "settings.parallel_audits must be in [1,16], got: $parallel"
    case "$color" in auto|always|never) ;; *) common_die "settings.color must be auto|always|never, got: $color" ;; esac

    # sites
    local sites_n i
    sites_n=$(printf '%s' "$json" | jq '(.sites // []) | length')

    # Track names for uniqueness.
    declare -A seen=()
    for ((i=0; i<sites_n; i++)); do
        local s name path type composer_path npm_path enabled
        s=$(printf '%s' "$json" | jq -c ".sites[$i]")
        name=$(printf '%s' "$s" | jq -r '.name // ""')
        path=$(printf '%s' "$s" | jq -r '.path // ""')
        type=$(printf '%s' "$s" | jq -r '.type // ""')
        composer_path=$(printf '%s' "$s" | jq -r '.composer_path // ""')
        npm_path=$(printf '%s' "$s"      | jq -r '.npm_path // ""')
        enabled=$(printf '%s' "$s"       | jq -r '.enabled // true')

        [[ -n "$name" ]] || common_die "sites[$i]: name must be non-empty"
        [[ -z "${seen[$name]:-}" ]] || common_die "duplicate site name: $name"
        seen[$name]=1

        [[ "$path" == /* ]] || common_die "sites[$name].path must be an absolute path, got: $path"
        case "$type" in composer|npm|both) ;; *) common_die "sites[$name].type must be composer|npm|both, got: $type" ;; esac
        [[ -z "$composer_path" || "$composer_path" == /* ]] || common_die "sites[$name].composer_path must be absolute, got: $composer_path"
        [[ -z "$npm_path"      || "$npm_path"      == /* ]] || common_die "sites[$name].npm_path must be absolute, got: $npm_path"
        case "$enabled" in true|false) ;; *) common_die "sites[$name].enabled must be true|false, got: $enabled" ;; esac
    done
}

# Load + validate + cache to a global for the current process.
# Sets WEBAUDT_CONFIG_JSON. Idempotent.
config_get() {
    if [[ -z "${WEBAUDT_CONFIG_JSON:-}" ]]; then
        WEBAUDT_CONFIG_JSON=$(config_load_json)
        printf '%s' "$WEBAUDT_CONFIG_JSON" | config_validate
        # Propagate color setting into env so common_use_color sees it.
        local color
        color=$(printf '%s' "$WEBAUDT_CONFIG_JSON" | jq -r '.settings.color // "auto"')
        export WEBAUDT_COLOR="$color"
    fi
    printf '%s' "$WEBAUDT_CONFIG_JSON"
}

# Helpers.
config_setting() { config_get | jq -r --arg k "$1" --arg d "$2" '.settings[$k] // $d'; }
config_sites()   { config_get | jq -c '(.sites // [])[]'; }
config_site_by_name() {
    local name="$1"
    config_get | jq -c --arg n "$name" '(.sites // [])[] | select(.name == $n)'
}

# Write config JSON back to TOML on disk. Reads JSON on stdin.
config_write_json() {
    local file tmp
    file="$(common_config_file)"
    tmp=$(mktemp)
    yq -p json -o toml '.' >"$tmp"
    mv "$tmp" "$file"
    unset WEBAUDT_CONFIG_JSON
}
