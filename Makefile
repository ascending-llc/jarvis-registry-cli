.DEFAULT_GOAL := all

CLI_OUTPUT ?= bin/jarvis-registry

build:
	@go build -o "$(CLI_OUTPUT)" ./cmd/jarvis-registry
.PHONY: build

test:
	@go test -race ./...
.PHONY: test

test-personal-scope:
	@go test -race ./skills -run 'Test(Symlink|PruneDanglingLinks|SyncCommandRunPersonalScope|SyncCommandPersonalScopeInteractiveValidation|ManifestReadWriterReplacesReadOnlyManifest)'
.PHONY: test-personal-scope

test-junction:
	@go test github.com/nyaosorg/go-windows-junction
.PHONY: test-junction

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
