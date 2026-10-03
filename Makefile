# SPDX-License-Identifier: Apache-2.0
GO ?= go
GOVULNCHECK_VERSION := v1.8.0

.PHONY: fmt vet test test-scripts race build check audit vulncheck guest oci-guest test-kvm bench-t01 net-helper storage-probe

# Check formatting without modifying source files.
fmt:
	@files=$$(gofmt -l cmd internal experiments/boot) || exit 1; \
		test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-scripts:
	bash scripts/bootstrap_test.sh

race:
	CGO_ENABLED=1 $(GO) test -race ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -o bin/grillo ./cmd/grillo
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -buildvcs=false -o bin/grillo-agent ./cmd/grillo-agent

check: fmt vet test test-scripts race build audit

audit:
	$(GO) mod verify
	$(GO) list -m all
	$(GO) mod tidy -diff

# Explicit opt-in: downloads and executes this pinned official Go audit tool.
vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Build the T01 experiment guest kernel and initramfs (see experiments/boot).
guest:
	bash experiments/boot/build-guest.sh

# Build the T02 OCI experiment guest (runc + busybox bundles + initramfs).
oci-guest:
	bash experiments/boot/fetch-oci.sh
	bash experiments/boot/build-oci-guest.sh

# Real hardware test. Builds the guests, then boots microVMs under KVM.
# A missing /dev/kvm is reported as a SKIP by the tests, never as a pass.
test-kvm: guest oci-guest
	go test -tags kvm -count=1 -v ./experiments/boot/spike/ ./experiments/boot/oci/

# 30-cycle measured boot/stop run (T01 evidence). Writes no committed artifacts.
bench-t01: guest
	go run ./experiments/boot/run -cycles 30

# Rootless networking-helper probe (T01). Requires a helper and host egress.
net-helper:
	sh experiments/boot/netns-helper.sh

# T02 storage/live-bind feasibility probe: documents the Firecracker limitation.
storage-probe:
	sh experiments/boot/storage-probe.sh
