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
```

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

## Scope

v0.1 is read-only: register, audit, view. Update / commit / push workflows are deferred. See `webaudt-prd.md` §12.

## License

MIT — see `LICENSE`.
