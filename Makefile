.DEFAULT_GOAL := all

test:
	@go test -race ./...
.PHONY: test

coverage:
	@go test -race -coverprofile=coverage.out ./...
.PHONY: coverage

show: coverage
	@go tool cover -html=coverage.out
.PHONY: show

lint:
	@pre-commit run --all-files golangci-lint-full
.PHONY: lint

verify:
	@pre-commit run --all-files golangci-lint-config-verify
.PHONY: verify

fmt:
	@pre-commit run --all-files golangci-lint-fmt
.PHONY: fmt

lint-all:
	@pre-commit run --all-files
.PHONY: lint-all

tartufo:
	@pre-commit run --all-files tartufo
.PHONY: tartufo

all: test lint
.PHONY: all
