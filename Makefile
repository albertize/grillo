# SPDX-License-Identifier: Apache-2.0
GO ?= go
export GOTOOLCHAIN := local
GOVULNCHECK_VERSION := v1.8.0
# Development identity by default; release callers must provide locked metadata.
VERSION ?= dev
COMMIT ?= $(shell git rev-parse HEAD)
BUILD_TIME ?= unknown
HOST_LDFLAGS = -X github.com/albertize/grillo/internal/buildinfo.Version=$(VERSION) -X github.com/albertize/grillo/internal/buildinfo.Commit=$(COMMIT) -X github.com/albertize/grillo/internal/buildinfo.BuildTime=$(BUILD_TIME)

.PHONY: fmt vet test test-scripts race build check audit vulncheck guest oci-guest test-kvm bench-t01 net-helper storage-probe qemu-guest test-qemu qemu-share

# Check formatting without modifying source files.
fmt:
	@files=$$(gofmt -l cmd internal guest experiments/boot scripts) || exit 1; \
		test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-scripts:
	bash scripts/bootstrap_test.sh
	bash scripts/fetch_oci_test.sh
	bash scripts/payload_test.sh
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/go_toolchain_test.py
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/prepare_env_test.py

race:
	CGO_ENABLED=1 $(GO) test -race ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(HOST_LDFLAGS)' -o bin/grillo ./cmd/grillo
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(HOST_LDFLAGS)' -o bin/grillod ./cmd/grillod
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(HOST_LDFLAGS)' -o bin/grillo-netns ./cmd/grillo-netns
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -buildvcs=false -ldflags '$(HOST_LDFLAGS)' -o bin/grillo-agent ./cmd/grillo-agent

# Frontend downloads are explicit. npm ci verifies the committed integrity lock;
# lifecycle scripts are disabled (including native-tool post-install downloads).
ui-deps:
	cd web && npm ci --ignore-scripts --no-audit --no-fund

ui-build:
	@test -d web/node_modules/esbuild || { echo 'Frontend dependencies missing: run make ui-deps (explicit locked download).'; exit 1; }
	cd web && npm run build

ui-check: ui-build
	cd web && npm test && npm run check

# Embed freshly built assets even when make runs with parallel prerequisites.
vet test race build test-ui-browser test-ui test-helm: ui-build

.PHONY: ui-deps ui-build ui-check

.PHONY: check-go
check-go:
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/go-toolchain.py --go '$(GO)'

# Require the exact selected 1.27 patch, never an automatic toolchain download.
vet test race build audit fuzz: check-go

check: check-go ui-check fmt vet test test-scripts race build audit

audit:
	$(GO) mod verify
	$(GO) list -m all
	$(GO) mod tidy -diff

# Bounded smoke by default; opt into a longer local campaign with
# make fuzz FUZZTIME=10m. Separate processes select exactly one target each.
FUZZTIME ?= 5s
FUZZWORKERS ?= 2
.PHONY: fuzz
fuzz:
	$(GO) test ./internal/guestproto -run '^$$' -fuzz '^FuzzReadFrame$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZWORKERS)
	$(GO) test ./internal/guestproto -run '^$$' -fuzz '^FuzzReadMessage$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZWORKERS)
	$(GO) test ./internal/frontend/kubernetes -run '^$$' -fuzz '^FuzzCompile$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZWORKERS)
	$(GO) test ./internal/network -run '^$$' -fuzz '^FuzzDNSRespond$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZWORKERS)
	$(GO) test ./internal/oci -run '^$$' -fuzz '^FuzzUnpackLayer$$' -fuzztime=$(FUZZTIME) -parallel=$(FUZZWORKERS)

# Bounded cache contention/cancellation; no KVM or eviction.
.PHONY: test-cache-load test-license-inventory
test-cache-load:
	$(GO) test -race -count=100 -run 'TestCASQuota|TestCacheGuard' ./internal/oci/
	$(GO) test -race -count=100 -run 'TestOCIResolverAggregateQuota|TestOCIResolverIndependentInstances' ./internal/executor/

# Optional development audit tooling uses Python stdlib, not a runtime dependency.
test-license-inventory:
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/license_inventory_test.py

# Explicit opt-in: downloads and executes this pinned official Go audit tool.
vulncheck:
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/go-toolchain.py --go '$(GO)' --scanner '$(GOVULNCHECK_VERSION)'

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

# D0 runtime-only guest. Inputs must already be provisioned; no implicit fetch.
RUNTIME_STORE ?= experiments/artifacts/runtime
RUNTIME_SELECTION ?= experiments/artifacts/runtime-selected.txt
GUEST_KERNEL ?= experiments/artifacts/qemu/bzImage
GUEST_INPUTS ?= experiments/artifacts/t02
.PHONY: runtime-guest
runtime-guest: build
	@mkdir -p "$$(dirname '$(RUNTIME_SELECTION)')"
	$(GO) run ./guest/runtime-image -agent bin/grillo-agent -kernel '$(GUEST_KERNEL)' -kernel-version 6.1.188 -inputs '$(GUEST_INPUTS)' -store '$(RUNTIME_STORE)' -version '$(VERSION)' -commit '$(COMMIT)' -selection '$(RUNTIME_SELECTION)'

# One local experimental payload, not redistribution-cleared release packaging.
STAGE_PREFIX ?= experiments/artifacts/stage
HELM_BINARY ?=
HELM_SHA256 ?=
PAYLOAD_ROOT ?= experiments/artifacts/payloads
.PHONY: stage-runtime test-installed-runtime payload
# New uniquely named archive/checksum pair on every run; never reuse STAGE_PREFIX.
payload: runtime-guest
	GO='$(GO)' bash scripts/payload.sh '$(PAYLOAD_ROOT)' '$(RUNTIME_SELECTION)' '$(HELM_BINARY)' '$(HELM_SHA256)'

stage-runtime: runtime-guest
	@test -n '$(HELM_BINARY)' -a -n '$(HELM_SHA256)' || { echo 'Provide an explicit trusted HELM_BINARY and HELM_SHA256; no tool download is automatic.'; exit 1; }
	$(GO) run ./scripts/stage -out '$(STAGE_PREFIX)' -guest-selection '$(RUNTIME_SELECTION)' -helm '$(HELM_BINARY)' -helm-sha256 '$(HELM_SHA256)'

test-installed-runtime: runtime-guest
	@test -n '$(HELM_BINARY)' -a -n '$(HELM_SHA256)' || { echo 'Provide an explicit trusted HELM_BINARY and HELM_SHA256.'; exit 1; }
	GRILLO_TEST_HELM_BINARY='$(HELM_BINARY)' GRILLO_TEST_HELM_SHA256='$(HELM_SHA256)' GRILLO_TEST_GUEST_SELECTION='$(RUNTIME_SELECTION)' $(GO) test -tags kvm -count=1 -v -timeout 600s -run '^TestKVMInstalledRuntime$$' ./internal/distribution/

# T24 warm production-path measurements: verified snapshots and fresh keys.
# No application/network/idle-system budget or clean-host claim follows.
.PHONY: test-t24-verified-lifecycle
test-t24-verified-lifecycle: runtime-guest
	GRILLO_TEST_GUEST_SELECTION='$(RUNTIME_SELECTION)' $(GO) test -tags kvm -count=1 -v -timeout 600s -run '^TestKVMVerifiedRuntimeLifecycle$$' ./internal/backend/qemu/

# D0 portable inventory gate with a rebuilt development fixture, not a release guest.
.PHONY: test-d0-manifest
test-d0-manifest: t07-guest
	go test -tags kvm -count=1 -v -timeout 180s -run '^TestKVMPortableManifestBoot$$' ./internal/backend/qemu/

# T10 storage: real bind persistence and read-only enforcement (scenario G)
# through the backend and guest agent. Missing /dev/kvm or the image is a SKIP.
test-t10: t07-guest
	go test -tags kvm -count=1 -v -timeout 180s -run TestKVMBindPersistence ./internal/storage/

# End-to-end: apply a native manifest through the reconciler and executor on
# real KVM (boot sandbox, start container, status, exec, down).
test-executor: t07-guest oci-guest
	go test -tags kvm -count=1 -v -timeout 180s -run 'TestKVMEndToEndApply|TestKVMLivenessRestart' ./internal/executor/

# T09 live registry: pull a digest-pinned image from a real registry.
test-netreg:
	go test -tags netreg -count=1 -v -timeout 300s -run TestLivePullPinnedImage ./internal/oci/

# Production rootless topology: per-application pasta namespace + bridge +
# per-sandbox TAP; cross-VM reachability, real DNS addresses, egress, isolation.
test-bridged: t07-guest oci-guest
	go test -tags kvm -count=1 -v -timeout 300s -run TestKVMBridgedApplicationNetwork ./internal/executor/

# T11 networking: real rootless helper (pasta) with egress, application isolation,
# and management unreachability. Missing pasta/userns/loopback is a SKIP.
test-netns:
	go test -tags netns -count=1 -v -timeout 180s -run TestPastaEgressAndIsolation ./internal/network/

# T18 builder: real rootless Podman build, OCI layout import, and image GC.
# Missing Podman is a documented SKIP.
test-builder:
	go test -tags builder -count=1 -v -timeout 300s ./internal/build/ ./internal/image/ ./internal/oci/

# T19 F2 gate: a complete Compose application (web + worker + volume + DNS) on
# the real runtime, including up, re-apply, down, recovery, and persistence.
test-f2: t07-guest
	go test -tags kvm -count=1 -v -timeout 300s -run TestKVMComposeF2Application ./internal/executor/

# T20 Kubernetes MVP compiler: a compiled multi-container Pod on the real runtime.
test-k8s: t07-guest
	go test -tags kvm -count=1 -v -timeout 300s -run TestKVMKubernetesMultiContainer ./internal/executor/

# T22 full scenario C hardware gate. Missing datapaths must fail, not skip.
test-helm: t07-guest
	go test -tags kvm -count=1 -v -timeout 360s -run 'TestKVMHelmApplication|TestKVMHelmScenarioC' ./internal/executor/
	go test -tags kvm -count=1 -v -timeout 300s -run TestKVMDaemonRemediation ./cmd/grillod/

# T23: isolated Chrome/Firefox rendering fixtures; no downloaded browser/driver.
test-ui-browser:
	go test -tags browser -count=1 -v -timeout 180s -run 'TestChromeConsole|TestFirefoxConsole' ./internal/ui/

# T23: actual CLI UI + daemon + real KVM workload, exec, metrics and lifetime.
test-ui: t07-guest
	go test -tags kvm -count=1 -v -timeout 360s -run TestKVMDaemonUI ./cmd/grillod/

# Startup-only Compose ordering/health on real KVM; uses the prepared OCI fixture.
test-compose-dependencies: t07-guest
	go test -tags kvm -count=1 -v -timeout 180s -run TestKVMComposeDependencyHealth ./internal/executor/

.PHONY: test-ui test-ui-browser test-compose-dependencies

# T18b native builder: real RUN execution inside a sandboxed guest.
# Missing /dev/kvm or guest artifacts is a documented SKIP.
test-builder-kvm: t07-guest
	go test -tags kvm -count=1 -v -timeout 240s -run 'TestKVMBuildGuestRun|TestKVMBuildNativeBuilderRun|TestKVMBuildNativeMultiStage|TestKVMBuildGuestNetwork' ./internal/build/

# Full T02/T03 real hardware evidence. No downloads or preexisting volume deletion.
.PHONY: f0-guest test-f0 t07-guest test-t07 test-t08 test-t10 test-netns test-executor test-netreg test-bridged test-builder test-builder-kvm test-f2 test-k8s test-helm
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
