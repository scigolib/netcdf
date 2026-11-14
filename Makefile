# netcdf Makefile
# Pure Go NetCDF reader and writer.
#
# The checks CI runs are in .github/workflows/ci.yml; `make ci` runs the same
# gates locally (formatting, build, tests, lint). Outputs go to tmp/, which is
# ignored by git.

PROJECT = netcdf

.DEFAULT_GOAL := test

build:
	@echo "Building $(PROJECT)..."
	go build ./...

test:
	@echo "Running tests..."
	@mkdir -p tmp
	go test -coverprofile=tmp/coverage.out ./...

test-race:
	@echo "Running tests with race detector..."
	go test -race ./...

lint:
	@echo "Running linter..."
	golangci-lint run --timeout=5m ./...

fmt:
	@echo "Formatting code..."
	gofmt -w -s .
	go mod tidy

fmt-check:
	@echo "Checking code formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "ERROR: The following files are not formatted:"; \
		gofmt -l .; \
		echo ""; \
		echo "Run 'make fmt' to fix formatting issues."; \
		exit 1; \
	fi
	@echo "All files are formatted."

clean:
	@echo "Cleaning..."
	rm -f tmp/coverage.out tmp/coverage.html

# The gates CI runs (formatting, build, tests, lint)
ci: fmt-check build test lint
	@echo "CI checks passed."

.PHONY: build test test-race lint fmt fmt-check clean ci
