# SPDX-License-Identifier: Apache-2.0
GO ?= go
GOVULNCHECK_VERSION := v1.8.0

.PHONY: fmt vet test test-scripts race build check audit vulncheck guest oci-guest test-kvm bench-t01 net-helper storage-probe qemu-guest test-qemu qemu-share

# Check formatting without modifying source files.
fmt:
	@files=$$(gofmt -l cmd internal guest experiments/boot) || exit 1; \
		test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-scripts:
	bash scripts/bootstrap_test.sh
	bash scripts/fetch_oci_test.sh

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
	go test -tags kvm -count=1 -v -run 'TestKVMExecBootStop|TestKVMExecMissingCommand|TestKVMOCIScenarios' ./experiments/boot/spike/ ./experiments/boot/oci/

# 30-cycle measured boot/stop run (T01 evidence). Writes no committed artifacts.
bench-t01: guest
	go run ./experiments/boot/run -cycles 30

# Rootless networking-helper probe (T01). Requires a helper and host egress.
net-helper:
	sh experiments/boot/netns-helper.sh

# T02 storage/live-bind feasibility probe: documents the Firecracker limitation.
storage-probe:
	sh experiments/boot/storage-probe.sh

# T03 backend-comparison artifacts: QEMU guest kernel with VIRTIO_FS + probe initramfs.
qemu-guest:
	bash experiments/boot/qemu/build-kernel.sh
	bash experiments/boot/qemu/build-initramfs.sh
	PROBE_PKG=./experiments/boot/qemu/netprobe PROBE_NAME=initramfs-net.cpio.gz bash experiments/boot/qemu/build-initramfs.sh

# Real KVM tests for the QEMU backend: virtiofs live sharing, rootless networking,
# and the OCI scenarios. These cover the gate Firecracker failed.
test-qemu: qemu-guest oci-guest
	go test -tags kvm -count=1 -v -run 'TestKVMQEMU' ./experiments/boot/qemu/run/ ./experiments/boot/oci/

# T07 guest image: real agent + static runc + busybox rootfs + key + manifest.
t07-guest:
	bash guest/build-image.sh

# Real KVM scenario A for the guest agent (init container, sidecar localhost,
# separate roots, status, stop). Missing /dev/kvm or artifacts is a SKIP.
test-t07: t07-guest
	go test -tags kvm -count=1 -v -run TestKVMAgent ./internal/guest/

# T08 backend lifecycle: real create/start/inspect/stop/delete cycles with no
# leaked VMM processes or directories. GRILLO_KVM_CYCLES=100 runs the 100-cycle
# gate (~90s). Missing /dev/kvm or the T07 image is a SKIP.
test-t08: t07-guest
	go test -tags kvm -count=1 -v -timeout 600s -run TestKVMCreateStartStopDelete ./internal/backend/qemu/

# T10 storage: real bind persistence and read-only enforcement (scenario G)
# through the backend and guest agent. Missing /dev/kvm or the image is a SKIP.
test-t10: t07-guest
	go test -tags kvm -count=1 -v -timeout 180s -run TestKVMBindPersistence ./internal/storage/

# End-to-end: apply a native manifest through the reconciler and executor on
# real KVM (boot sandbox, start container, status, exec, down).
test-executor: t07-guest oci-guest
	go test -tags kvm -count=1 -v -timeout 180s -run TestKVM ./internal/executor/

# T11 networking: real rootless helper (pasta) with egress, application isolation,
# and management unreachability. Missing pasta/userns/loopback is a SKIP.
test-netns:
	go test -tags netns -count=1 -v -timeout 180s -run TestPastaEgressAndIsolation ./internal/network/

# Full T02/T03 real hardware evidence. No downloads or preexisting volume deletion.
.PHONY: f0-guest test-f0 t07-guest test-t07 test-t08 test-t10 test-netns test-executor
f0-guest:
	bash experiments/boot/qemu/build-kernel.sh
	bash experiments/boot/qemu/build-f0-guest.sh

test-f0: f0-guest
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -o bin/grillo-f0 ./experiments/boot/qemu/run
	./bin/grillo-f0 -scenario f0 -initramfs experiments/artifacts/qemu/initramfs-f0.cpio.gz -timeout 10m

# Print the guest serial console from the QEMU live-share experiment.
qemu-share: qemu-guest
	go run ./experiments/boot/qemu/run -verbose
