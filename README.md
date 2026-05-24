# webaudt

Terminal UI for monitoring `composer audit` and `npm audit` findings across a registry of local sites.

## Install

```sh
git clone <this repo> ~/src/webaudt
cd ~/src/webaudt
./install.sh     # symlinks bin/webaudt into ~/.local/bin
webaudt doctor   # verify dependencies
```

### Dependencies

Required: `bash >= 4`, `fzf >= 0.40`, `gum >= 0.13`, `jq >= 1.6`, `yq >= 4` (Mike Farah's Go build, **not** the Python one), `git >= 2.20`.

Optional, only invoked for sites of that type: `composer`, `npm`.

macOS:

```sh
brew install bash fzf gum jq yq git
```

Debian/Ubuntu:

```sh
sudo apt install bash fzf jq git
# gum: github.com/charmbracelet/gum
# yq:  github.com/mikefarah/yq (v4)
```

Arch:

```sh
sudo pacman -S bash fzf gum jq go-yq git
```

## Quickstart

```sh
webaudt add ~/Sites/mysite.com       # auto-detects composer/npm/both
webaudt list                          # show registered sites
webaudt                               # open TUI
webaudt refresh --all                 # re-run all audits
webaudt status --json | jq .          # scriptable output
webaudt rm mysite.com                 # remove a site
```

## Configuration

`~/.config/webaudt/config.toml` is created with defaults on first run. See `webaudt-prd.md` §5 for the full schema.

```toml
[settings]
cache_ttl = 3600
parallel_audits = 4
composer_bin = "composer"
npm_bin = "npm"
color = "auto"

[[sites]]
name = "mysite"
path = "/home/user/Sites/mysite.com"
type = "both"
composer_path = "/home/user/Sites/mysite.com"
npm_path = "/home/user/Sites/mysite.com/www"
# composer_bin / npm_bin are auto-detected at `add` time and only persisted
# when they differ from the global default. Edit freely to point at a
# project-local binary (e.g. `bin/composer`, `composer.phar`, or a specific
# `~/.nvm/versions/node/<v>/bin/npm`).
composer_bin = "/home/user/Sites/mysite.com/bin/composer"
```

## Shell completion

**zsh:**

```sh
mkdir -p ~/.zsh/completions
ln -sf "$PWD/completions/_webaudt" ~/.zsh/completions/_webaudt
# Add to ~/.zshrc (before `compinit`):
#   fpath=(~/.zsh/completions $fpath)
#   autoload -Uz compinit && compinit
```

**bash:**

```sh
echo "source $PWD/completions/webaudt.bash" >> ~/.bashrc
```

Completion covers subcommands, flags, ecosystem types, and dynamic site names from your config.

## TUI keys

| Key   | Action                                  |
|-------|-----------------------------------------|
| `r`   | Refresh the highlighted site            |
| `R`   | Refresh every site (ignores TTL)        |
| Enter | Full JSON details (pager / `bat`)       |
| Esc   | Quit                                    |

## Exit codes (`webaudt status`)

| Code | Meaning                  |
|------|--------------------------|
| 0    | Clean                    |
| 1    | Moderate / low advisories|
| 2    | High advisories          |
| 3    | Critical advisories      |
| 10   | Audit failed             |

## Portability

Tested on macOS (Apple Silicon, bash 5.x via Homebrew) and designed to work on Linux (bash 4+). Key portability notes:

- Uses `sha1sum` on Linux, `shasum` on macOS — whichever is available.
- Uses BSD `date -r <epoch>` on macOS, GNU `date -d @<epoch>` on Linux.
- Advisory locks use `mkdir` (atomic on every POSIX filesystem), no `flock` dep.
- Per-ecosystem binaries (`composer`, `npm`) are only required for sites of that type — not at install time.

macOS users **must** install bash 4+ (`brew install bash`) since the system bash is 3.2 and lacks features like `wait -n` and `declare -A`. The installer warns if the bash on your `$PATH` is too old.

## Scope

v0.1 is read-only: register, audit, view. Update / commit / push workflows are deferred. See `webaudt-prd.md` §12.

## License

MIT — see `LICENSE`.
