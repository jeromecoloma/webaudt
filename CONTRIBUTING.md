# Contributing to webaudt

Thanks for your interest. This project is **active** and welcomes contributions.

## Development setup

Requires Go 1.21+ (project currently builds with Go 1.26).

```sh
git clone https://github.com/jeromecoloma/webaudt.git
cd webaudt
go build -o bin/webaudt ./cmd/webaudt
./bin/webaudt --help
```

To install locally for testing:

```sh
./install.sh
```

## Running tests

```sh
go test ./...
```

## Linting

```sh
go vet ./...
gofmt -l .
```

If you have [golangci-lint](https://golangci-lint.run/) installed:

```sh
golangci-lint run
```

CI runs all of the above on every PR.

## Submitting a pull request

1. Fork the repo and create a topic branch from `main`.
2. Make your change. Keep it focused — one logical change per PR.
3. Add or update tests where it makes sense.
4. Run `go test ./...`, `go vet ./...`, and `gofmt -w .` before pushing.
5. Open a PR against `main`. Fill out the PR template.
6. Expect an initial review within 2 weeks. Iterate as needed.

## What's in scope

- Bug fixes
- New audit sources that fit the "local site registry" model
- TUI ergonomics and keybindings
- Improvements to output formatting and exit codes
- Cross-platform fixes (Linux, BSD)

## What's out of scope

- Remote/cloud-hosted audit collection
- Replacing the TUI with a web UI
- Language ecosystems beyond composer/npm without prior discussion
- Anything that introduces a runtime dependency on a non-Go service

If unsure, open a discussion or draft issue before investing time.

## Commit messages

Follow the existing style — short imperative subject (`fix:`, `feat:`, `style:`,
`refactor:`, `docs:` prefixes are fine but not required).

## Reporting bugs / requesting features

Use the issue templates in `.github/ISSUE_TEMPLATE/`. A `good first issue`
label is applied to newcomer-friendly tickets.
