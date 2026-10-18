# Contributing

Thanks for taking the time to improve go-cli-tools! Bug reports, small fixes and new ideas are all welcome.

## Getting started

You need Go 1.22 or newer and `make` (on Windows, use Git Bash or WSL for the Makefile).

```sh
git clone https://github.com/tonklasocute/go-cli-tools.git
cd go-cli-tools
make build        # binaries land in ./bin
make test         # unit + integration tests
make cover        # coverage report, fails below 80%
make lint         # golangci-lint (uses `go run` if it isn't installed)
```

## Project layout

- `cmd/<tool>/main.go` is a one-liner that hands off to `internal/<tool>`.
- `internal/<tool>/` holds all logic **and** the cobra command (`cli.go`), so commands can be tested in-process.
- `internal/ui`, `internal/output`, `internal/cliutil` and `internal/version` are shared by every tool. If you need
  colors, tables, sizes, durations, `--json` output or exit codes, use these rather than rolling your own.

## Conventions every tool follows

- `--help` with examples, `--version`, `--json`, `--no-color` (and `NO_COLOR`), no color when stdout isn't a TTY.
- Exit codes: `0` ok, `1` check failed, `2` usage/operational error, `130` interrupted.
- Progress indicators go to stderr and only when stderr is a terminal.
- No panics on bad input; errors are short, specific and actionable.

## Tests

- Prefer table-driven tests for logic and in-process CLI tests via `cliutil.Run`.
- Every tool also has an integration test in `cmd/<tool>/main_test.go` that builds and runs the real binary.
- Tests must pass with `go test -race ./...` on Linux, macOS and Windows. Don't use fixed ports (`net.Listen("tcp", "127.0.0.1:0")`)
  or sleeps for synchronisation.

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org): `feat(envcheck): add float rule`,
  `fix(reqping): handle empty body`, `docs: ...`, `test(dirsize): ...`. The release changelog is generated from these.
- Keep pull requests focused on one change and update `--help`, the README and `CHANGELOG.md` when behaviour changes.
- Make sure `make test lint` passes before opening the PR.

## Releasing (maintainers)

Releases are cut by pushing a `vX.Y.Z` tag; the `release` workflow runs GoReleaser. Try it locally first with
`make release-snapshot`.
