#!/usr/bin/env bash
# install.sh — symlink bin/webaudt into ~/.local/bin (idempotent).

set -euo pipefail

src="$(cd "$(dirname "$0")" && pwd)/bin/webaudt"
target_dir="${PREFIX:-$HOME/.local/bin}"
target="$target_dir/webaudt"

mkdir -p "$target_dir"

if [[ -L "$target" ]]; then
    existing=$(readlink "$target")
    if [[ "$existing" == "$src" ]]; then
        printf 'webaudt: already installed at %s\n' "$target"
        exit 0
    fi
    printf 'webaudt: replacing existing symlink (%s -> %s)\n' "$target" "$existing"
    rm "$target"
elif [[ -e "$target" ]]; then
    printf 'webaudt: %s exists and is not a symlink; refusing to overwrite\n' "$target" >&2
    exit 1
fi

ln -s "$src" "$target"
printf 'webaudt: installed -> %s\n' "$target"

case ":$PATH:" in
    *":$target_dir:"*) ;;
    *) printf '\nNote: %s is not in $PATH. Add it to your shell rc:\n  export PATH="%s:$PATH"\n' "$target_dir" "$target_dir" ;;
esac
