.DEFAULT_GOAL := all

test:
	@go test ./...
.PHONY: test

coverage:
	@go test -coverprofile=coverage.out ./...
.PHONY: coverage

show: coverage
	@go tool cover -html=coverage.out
.PHONY: show

lint:
	@pre-commit run --all-files golangci-lint-full
.PHONY: lint

verify:
	@pre-commit run --all-files golangci-lint-verify
.PHONY: verify

fmt:
	@pre-commit run --all-files golangci-lint-fmt
.PHONY: fmt

lint-all:
	@pre-commit run --all-files
.PHONY: all

tartufo:
	@pre-commit run tartufo
.PHONY: tartufo

all: test lint
.PHONY: all
