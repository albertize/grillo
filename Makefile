# SPDX-License-Identifier: Apache-2.0
GO ?= go
GOVULNCHECK_VERSION := v1.8.0

.PHONY: fmt vet test race build check audit vulncheck test-kvm

# Check formatting without modifying source files.
fmt:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	CGO_ENABLED=1 $(GO) test -race ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -o bin/grillo ./cmd/grillo
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -buildvcs=false -o bin/grillo-agent ./cmd/grillo-agent

check: fmt vet test race build audit

audit:
	$(GO) mod verify
	$(GO) list -m all
	$(GO) mod tidy -diff

# Explicit opt-in: downloads and executes this pinned official Go audit tool.
vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Fail closed until T01 adds real hardware tests. Never report zero tests as PASS.
test-kvm:
	@echo 'BLOCKED: no KVM integration tests or guest artifacts yet (T01). No hardware checks performed.' >&2
	@exit 1
