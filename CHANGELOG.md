# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-18

First public release: five standalone CLI tools sharing one set of conventions.

### Added

- **envcheck** — validate `.env` files against an annotated `.env.example` schema
  (`required`, `secret`, `int`, `float`, `bool`, `url`, `email`, `enum`, `regex`, `min`/`max`).
  Reports missing keys, type errors, unknown and duplicate keys and malformed lines with line numbers;
  secret values are masked everywhere. `envcheck init` generates a schema from an existing `.env`.
- **portscout** — concurrent TCP connect scanner with port ranges, a curated list of common dev ports,
  service names, latency, a progress bar, clean Ctrl+C handling and `--expect-open` for CI readiness checks.
- **jsonformat** — streaming JSON pretty-printer/minifier/validator with exact number preservation,
  `--sort-keys`, `--query a.b[0].c`, atomic in-place writes and `file:line:col` errors with a caret
  under the offending character.
- **dirsize** — concurrent directory size analyzer with a terminal bar chart, `--depth`, `--top`,
  `--min-size`, `--exclude` and `--files`; symlinks are not followed and hard links are counted once on Unix.
- **reqping** — concurrent HTTP health checker with expected statuses, body checks, custom headers,
  TLS certificate expiry warnings, `--count` latency statistics (min/avg/p95/max) and `--watch` mode.
- Shared behaviour for every tool: `--help` with examples, `--version`, `--json`, `--no-color` /
  `NO_COLOR`, automatic color only on terminals, and exit codes `0` ok / `1` check failed /
  `2` usage error / `130` interrupted.
- Makefile, GoReleaser config (linux/darwin/windows × amd64/arm64), GitHub Actions CI and release
  workflows, VHS demo tapes.

[Unreleased]: https://github.com/tonklasocute/go-cli-tools/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tonklasocute/go-cli-tools/releases/tag/v0.1.0
