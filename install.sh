#!/usr/bin/env bash
# install.sh — install webaudt.
#
# Two modes:
#   release  (default when piped from curl) — download the prebuilt binary for
#            this platform from GitHub Releases and verify its sha256 against
#            the release's checksums.txt before installing.
#   source   (default when run inside a checkout) — build with `go build` and
#            symlink, so a rebuild is picked up immediately.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jeromecoloma/webaudt/main/install.sh | bash
#   ./install.sh                     # from a checkout: build from source
#   ./install.sh --from-release      # force download of a prebuilt binary
#   ./install.sh --from-source       # force a source build
#   ./install.sh --version v0.3.4    # pin a release (implies --from-release)
#
# Env:
#   PREFIX   install dir (default ~/.local/bin)

set -euo pipefail

REPO="jeromecoloma/webaudt"
BIN="webaudt"

src_dir="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"
target_dir="${PREFIX:-$HOME/.local/bin}"
target="$target_dir/$BIN"

mode=""
pin_version=""

die() {
    printf '%s: %s\n' "$BIN" "$1" >&2
    exit 1
}

info() { printf '  %s\n' "$1"; }

while [ $# -gt 0 ]; do
    case "$1" in
        --from-source)  mode="source" ;;
        --from-release) mode="release" ;;
        --version)
            [ $# -ge 2 ] || die "--version needs a tag (e.g. --version v0.3.4)"
            pin_version="$2"
            mode="release"
            shift
            ;;
        --version=*)
            pin_version="${1#*=}"
            mode="release"
            ;;
        -h|--help)
            sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) die "unknown option: $1 (try --help)" ;;
    esac
    shift
done

# Default: build from source only if we're actually sitting in the checkout.
if [ -z "$mode" ]; then
    if [ -n "$src_dir" ] && [ -f "$src_dir/go.mod" ]; then
        mode="source"
    else
        mode="release"
    fi
fi

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but was not found"; }

# ---- sha256 ----

sha256_of() {
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    elif command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        die "no sha256 tool found (need shasum or sha256sum) — refusing to install unverified"
    fi
}

# ---- release install ----

install_from_release() {
    need curl
    need tar

    local os arch uname_s uname_m
    uname_s="$(uname -s)"
    uname_m="$(uname -m)"
    case "$uname_s" in
        Darwin) os="macos" ;;
        Linux)  os="linux" ;;
        *) die "unsupported OS: $uname_s (build from source with --from-source)" ;;
    esac
    case "$uname_m" in
        arm64|aarch64) arch="arm64" ;;
        x86_64|amd64)  arch="x86_64" ;;
        *) die "unsupported architecture: $uname_m (build from source with --from-source)" ;;
    esac

    local tag
    if [ -n "$pin_version" ]; then
        tag="$pin_version"
    else
        info "Resolving latest release..."
        # Follow the /releases/latest redirect rather than parsing the API, so
        # this needs no token and no jq.
        tag="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
            "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
        [ -n "$tag" ] || die "could not resolve the latest release tag"
    fi

    local version archive url base tmp
    version="${tag#v}"
    archive="${BIN}_${version}_${os}_${arch}.tar.gz"
    base="https://github.com/$REPO/releases/download/$tag"

    tmp="$(mktemp -d)"
    # shellcheck disable=SC2064  # expand tmp now, not at trap time
    trap "rm -rf '$tmp'" EXIT

    info "Downloading $archive ($tag)..."
    curl -fsSL -o "$tmp/$archive" "$base/$archive" \
        || die "download failed: $base/$archive"
    curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" \
        || die "could not download checksums.txt — refusing to install unverified"

    # Verify before we unpack anything.
    local want got
    want="$(awk -v f="$archive" '$2 == f || $2 == "*"f {print $1}' "$tmp/checksums.txt" | head -n1)"
    [ -n "$want" ] || die "$archive is not listed in checksums.txt — refusing to install unverified"
    got="$(sha256_of "$tmp/$archive")"
    if [ "$want" != "$got" ]; then
        printf '%s: CHECKSUM MISMATCH for %s\n  expected: %s\n  actual:   %s\nRefusing to install.\n' \
            "$BIN" "$archive" "$want" "$got" >&2
        exit 1
    fi
    info "sha256 verified."

    tar -xzf "$tmp/$archive" -C "$tmp" || die "could not extract $archive"
    [ -f "$tmp/$BIN" ] || die "$BIN not found inside $archive"

    mkdir -p "$target_dir"
    if [ -L "$target" ]; then
        info "replacing existing symlink at $target"
        rm -f "$target"
    fi
    # Install via a temp name + mv so we never leave a half-written binary, and
    # so replacing a running binary works.
    install_tmp="$target.new.$$"
    cp "$tmp/$BIN" "$install_tmp"
    chmod 0755 "$install_tmp"
    mv -f "$install_tmp" "$target"
    info "installed $tag -> $target"

    installed_bin="$target"
}

# ---- source install ----

install_from_source() {
    [ -n "$src_dir" ] && [ -f "$src_dir/go.mod" ] \
        || die "--from-source needs to run from a webaudt checkout (no go.mod next to install.sh)"
    need go

    local src version
    src="$src_dir/bin/$BIN"

    info "Building $BIN from source..."
    # Stamp the version from git so the binary does not report the "dev"
    # sentinel baked into main.go.
    version="$(cd "$src_dir" && git describe --tags --always --dirty 2>/dev/null || echo dev)"
    (cd "$src_dir" && go build -ldflags "-X main.Version=$version" -o "bin/$BIN" ./cmd/webaudt)

    mkdir -p "$target_dir"
    if [ -L "$target" ]; then
        existing="$(readlink "$target")"
        if [ "$existing" = "$src" ]; then
            info "already linked at $target"
        else
            info "replacing symlink ($target -> $existing)"
            rm -f "$target"
            ln -s "$src" "$target"
        fi
    elif [ -e "$target" ]; then
        die "$target exists and is not a symlink; refusing to overwrite"
    else
        ln -s "$src" "$target"
        info "installed -> $target"
    fi

    installed_bin="$src"
}

case "$mode" in
    release) install_from_release ;;
    source)  install_from_source ;;
esac

case ":$PATH:" in
    *":$target_dir:"*) ;;
    *) printf '\n  Note: %s is not in $PATH. Add to your shell rc:\n    export PATH="%s:$PATH"\n' \
        "$target_dir" "$target_dir" ;;
esac

# ---- Shell completion via cobra's generator ----

install_zsh_completion() {
    local zsh_comp_dir="$HOME/.zsh/completions"
    local zsh_file="$zsh_comp_dir/_$BIN"
    mkdir -p "$zsh_comp_dir"
    if "$installed_bin" completion zsh >"$zsh_file" 2>/dev/null; then
        info "zsh:  installed -> $zsh_file"
    fi

    local zshrc="$HOME/.zshrc"
    if [ -f "$zshrc" ] && grep -q '\.zsh/completions' "$zshrc"; then
        :
    else
        printf '\n  Add to ~/.zshrc (before any existing `compinit` line):\n'
        printf '    fpath=(~/.zsh/completions $fpath)\n'
        printf '    autoload -Uz compinit && compinit\n'
        printf '  Then restart your shell or run: exec zsh\n'
    fi
}

install_bash_completion() {
    local bash_comp_dir="${BASH_COMPLETION_USER_DIR:-$HOME/.local/share/bash-completion/completions}"
    local bash_file="$bash_comp_dir/$BIN"
    mkdir -p "$bash_comp_dir"
    if "$installed_bin" completion bash >"$bash_file" 2>/dev/null; then
        info "bash: installed -> $bash_file"
    fi
}

printf '\n  Shell completion:\n'
install_zsh_completion
install_bash_completion

printf '\n  Done. Run `%s doctor` to verify.\n' "$BIN"
