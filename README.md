# webaudt

**Status:** active — under regular development.

Terminal UI for monitoring `composer audit` and `npm audit` findings across a registry of local sites.

Written in Go. Pure single-binary install — no external runtime deps beyond `composer`/`npm` themselves (and only when you register a site of that type).

## Install

### Prebuilt binary (recommended — no Go required)

Download the archive for your platform from the
[latest release](https://github.com/jeromecoloma/webaudt/releases/latest),
extract, and move `webaudt` onto your `$PATH`:

```sh
# macOS (Apple Silicon)
curl -L -o webaudt.tar.gz https://github.com/jeromecoloma/webaudt/releases/latest/download/webaudt_$(curl -s https://api.github.com/repos/jeromecoloma/webaudt/releases/latest | grep tag_name | cut -d'"' -f4 | sed 's/^v//')_macos_arm64.tar.gz
tar -xzf webaudt.tar.gz
mv webaudt ~/.local/bin/
webaudt doctor
```

Releases include macOS (arm64, x86_64) and Linux (arm64, x86_64) builds, plus a
`checksums.txt`.

### From source

```sh
git clone https://github.com/jeromecoloma/webaudt.git ~/src/webaudt
cd ~/src/webaudt
./install.sh
```

`install.sh` builds the binary, symlinks it into `~/.local/bin/webaudt`, and installs shell completions for bash and zsh.

Build manually:

```sh
go build -o bin/webaudt ./cmd/webaudt
```

Requires Go 1.21+ to build. The compiled binary has no Go runtime dependency.

## Quickstart

```sh
webaudt add ~/Sites/mysite.com       # auto-detects composer/npm/both
webaudt list                          # show registered sites
webaudt                               # open the TUI
webaudt refresh --all                 # re-run every audit
webaudt status --json                 # scriptable output
webaudt rm mysite                     # remove a site
webaudt doctor                        # verify composer/npm installs
```

Run `webaudt <cmd> --help` for per-command flags.

## Configuration

`~/.config/webaudt/config.toml` is created with defaults on first run.

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
# composer_bin and npm_bin are auto-detected at `add` time and only persisted
# when they differ from the global default. Edit freely.
composer_bin = "/home/user/Sites/mysite.com/bin/composer"
```

## TUI keys

| Key       | Action                                  |
|-----------|-----------------------------------------|
| `1` / `2` | Focus sidebar / preview pane            |
| `Tab`     | Cycle pane focus                        |
| `j` / `k` | Move down / up in the sidebar           |
| `↑` / `↓` | Scroll the preview pane (when focused)  |
| `r`       | Refresh the highlighted site            |
| `R`       | Refresh every site                      |
| `?`       | Show key hints                          |
| `q` / Esc | Quit                                    |

## Exit codes (`webaudt status` / `refresh`)

| Code | Meaning                          |
|------|----------------------------------|
| 0    | Clean                            |
| 1    | Moderate / low / info / unrated  |
| 2    | High                             |
| 3    | Critical                         |
| 10   | Audit failed                     |

## Severity buckets

webaudt normalizes both composer and npm output into six severity buckets, ordered worst → least severe:

1. **critical** — confirmed CVSS critical
2. **high** — confirmed CVSS high
3. **unknown** (pink/magenta `◆`) — advisory present but composer/npm gave **no severity rating** (typical for FriendsOfPHP advisories without a CVSS score). Treat as "review required".
4. **moderate**
5. **low**
6. **info**

## Portability

Single Go binary. Tested on macOS (Apple Silicon); Linux supported.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for dev setup and the PR process.
Bug reports and feature requests go through the issue templates in
`.github/ISSUE_TEMPLATE/`.

By participating you agree to abide by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md). Please do not file
public issues for security reports.

## Governance

See [GOVERNANCE.md](GOVERNANCE.md) and [MAINTAINERS.md](MAINTAINERS.md).

## License

MIT — see `LICENSE`.
