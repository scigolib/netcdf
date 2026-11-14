# Contributing to netcdf

Thank you for your interest in contributing to scigolib/netcdf!

## Prerequisites

- **Go 1.25+** ([download](https://go.dev/dl/))
- Optional, for interop checks: the netCDF C tools (`ncdump`, `nccopy`) and Python with `h5py`

## Building & Testing

```bash
go build ./...                  # build all packages
go test ./...                   # run all tests
go test -v -run TestName        # run a single test
go test -cover ./...            # check coverage
```

The race detector needs cgo: `go test -race ./...` runs on Linux, macOS, or in WSL on Windows.

## Code Style

- Run `gofmt ./...` before every commit; exported identifiers have doc comments.
- Zero cgo and no external dependencies without a discussion first.
- The classic NetCDF formats are big-endian by definition: never assume the host byte order,
  decode with `encoding/binary` and an explicit order.
- Binary parsers check every length and offset against the input size before allocating.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat:`, `fix:`, `docs:`, `test:`, `chore:`.

## Pull Request Workflow

1. Branch from `main` and make your change with tests.
2. A bug fix starts with a test that fails before the fix; a parser change names the spec
   section or the `ncdump`/`nccopy` output it is checked against.
3. Verify:
   ```bash
   gofmt -l .      # must print nothing
   go test ./...
   golangci-lint run
   ```
4. Open the pull request against `main`. The `Required checks` gate must be green, and every
   pull request needs one approving review from someone other than its author.

## Reporting Bugs

- Include the file that reproduces the problem when you can (the smallest one that does), the
  format (CDF-1, CDF-2, CDF-5 or NetCDF-4), what you expected and what happened.
- Security issues go through [SECURITY.md](SECURITY.md), never a public issue.

## License

By contributing you agree that your contributions are licensed under the MIT License.
