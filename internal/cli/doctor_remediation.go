//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"

	"github.com/albertize/grillo/internal/runtimeassets"
)

func doctorRemediation(check *Check) {
	check.Why = "This prerequisite is needed for the experimental runtime or installed payload."
	check.Inspect = "grillo doctor --json"
	check.Fix = "Provision verified matching assets/dependencies explicitly; do not run workloads as root or weaken host policy."
	check.Docs = "docs/runtime-layout.md"
	if layout, err := runtimeassets.Discover(); err == nil {
		installed, _ := layout.Path("guide", "")
		if info, err := os.Stat(installed); err == nil && info.Mode().IsRegular() {
			check.Docs = installed
		} else if source := filepath.Join(layout.Prefix, "docs/runtime-layout.md"); source != "" {
			if info, err := os.Stat(source); err == nil && info.Mode().IsRegular() {
				check.Docs = source
			}
		}
	}
	switch check.Name {
	case "runtime-user":
		check.Why = "Host orchestration must use your unprivileged account."
		check.Inspect = "id -u"
		check.Fix = "Run ordinary Grillo commands as your normal user, never sudo grillo."
	case "kvm", "kvm-api":
		check.Why = "Hardware-isolated workloads require usable Linux KVM, not host containers."
		check.Inspect = "ls -l /dev/kvm; id"
		check.Fix = "Review CPU virtualization, kernel support and your device access with an administrator. Installation alone cannot grant hardware access; never chmod 666 the device."
	case "vhost-vsock":
		check.Why = "The private host/guest management channel uses vhost-vsock."
		check.Inspect = "ls -l /dev/vhost-vsock; id"
		check.Fix = "Review kernel/device support and account permissions explicitly; do not substitute a workload-network management socket."
	case "tun":
		check.Why = "Rootless sandbox networking requires TAP devices inside private namespaces."
		check.Inspect = "ls -l /dev/net/tun; id"
		check.Fix = "Review kernel TUN support and device access under the host's normal policy. Do not grant broad host capabilities."
	case "namespace-access", "user-namespaces":
		check.Why = "Networking must run in unprivileged user/network namespaces."
		check.Inspect = "sysctl user.max_user_namespaces; journalctl -k"
		check.Fix = "Review distribution namespace/MAC restrictions with an administrator. Unsupported policies are blockers; no automatic sysctl, rootful or security-policy fallback."
	case "mac-policy":
		check.Why = "The recorded Fedora Enforcing policy prevents pasta networking."
		check.Inspect = "getenforce; journalctl -k"
		check.Fix = "Keep policy unchanged. This configuration is currently unsupported pending an Enforcing E2E fix; do not disable SELinux as an installation step."
	case "qemu-devices", "qemu":
		check.Why = "The selected backend needs microvm, virtio-fs, vsock, virtio-net and virtio-block devices."
		check.Inspect = "qemu-system-x86_64 -no-user-config -machine help; qemu-system-x86_64 -no-user-config -device help"
		check.Fix = "Provision a distro-managed QEMU exposing the required devices; no private VMM download or backend fallback."
	case "qemu-version":
		check.Why = "Only the recorded dependency configuration has local integration evidence."
		check.Inspect = "qemu-system-x86_64 --version"
		check.Fix = "Validate your dependency set with actual KVM E2E before claiming compatibility; do not infer a supported range from compilation."
	case "virtiofsd", "virtiofsd-options":
		check.Why = "Container roots and declared host volumes use virtiofs sharing."
		check.Inspect = "grillo doctor --json"
		check.Fix = "Provision a compatible distro-managed virtiofsd, or select an explicit absolute GRILLO_VIRTIOFSD_BINARY. Review --socket-path, --shared-dir and --sandbox support."
	case "helm-helper", "helm-version":
		check.Why = "Charts require the exact pinned renderer, not an arbitrary PATH tool."
		check.Inspect = "grillo plan <local-chart> --release <name>"
		check.Fix = "Re-stage the explicitly trusted official Helm v4.2.2 helper, or use GRILLO_HELM_BINARY for development. Rendering suppresses potentially sensitive tool output."
	case "guest-assets":
		check.Why = "Boot files must match a versioned inventory, host ABI/platform and pinned digests."
		check.Inspect = "grillo version --json; grillo doctor --json"
		check.Fix = "Re-stage one matching verified host/guest build. Never edit expectations to conceal mutation or fall back to an unverified guest. Explicit legacy manifests are development-only."
	case "daemon-helper", "netns-helper":
		check.Why = "The CLI uses installed private helpers without CWD/PATH hijacking."
		check.Inspect = "grillo version --json"
		check.Fix = "Restore the complete prefix-relative payload or validate explicit developer helper overrides; do not execute project-local lookalikes."
	case "state":
		check.Why = "Management sockets and user-owned runtime state must remain private."
		check.Inspect = "grillo doctor --json"
		check.Fix = "Review the reported XDG directory ownership/access. Grillo will create new private directories on first runtime startup, but does not chmod an unsafe existing runtime directory."
	case "host-support":
		check.Why = "No release or supported clean-host matrix exists yet."
		check.Inspect = "grillo version --json"
		check.Fix = "Treat this build as experimental. Installation, host policy, provenance and external-host gates must pass before support or redistribution claims."
	}
}
