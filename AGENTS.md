# Repository Guidelines

## Project Structure & Module Organization

This is a small Go command-line tool for converting `geosite.dat` into Surge `.list` rule files. Source files live at the repository root:

- `main.go`: CLI flags, download handling, and orchestration.
- `geosite.go` and `wire.go`: protobuf wire parsing for `geosite.dat`.
- `converter.go` and `regex.go`: geosite resolution and Surge rule conversion.
- `*_test.go`: unit tests colocated with the code they cover.

Generated or local-only files are ignored: `geosite.dat`, `geosite-surge`, `surge-rules/`, `*.test`, and `coverage.out`.

## Build, Test, and Development Commands

- `go test ./...`: run the full test suite.
- `go test -cover ./...`: run tests with coverage reporting.
- `go run . -codes steam -out surge-rules`: generate selected Surge lists locally.
- `go run . -out surge-rules`: generate all available lists, downloading `geosite.dat` if missing.
- `go build -o geosite-surge .`: build the CLI binary.
- `gofmt -w *.go`: format Go source before committing.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms. Keep package-level helpers small and focused, prefer clear names such as `readGeoSiteList`, `buildRuleFiles`, and `surgeRulesForRegex`, and keep tests table-driven when checking multiple conversion cases. Avoid unrelated refactors when changing parser or converter behavior.

## Testing Guidelines

Tests use Go’s built-in `testing` package. Add or update `*_test.go` files whenever conversion behavior, include resolution, regex handling, or protobuf parsing changes. For regex changes, include cases for exact conversion, expanded finite patterns, wildcard fallback, and unsupported `URL-REGEX` fallback.

## Commit & Pull Request Guidelines

Recent commits use short imperative summaries, for example `Optimize regex rule conversion` and `Rename project to geosite-surge`. Follow that style: concise, present-tense, and focused on one change.

Pull requests should include a short description, test results such as `go test ./...`, and notes on generated output changes when rule conversion behavior changes. Link related issues when available.

## Security & Configuration Tips

Do not commit downloaded `geosite.dat` files or generated `surge-rules/` output. Network access is only needed when downloading `geosite.dat`; prefer reusing a local file during tests and comparisons.
