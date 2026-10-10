//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/albertize/grillo/internal/guest"
	"github.com/albertize/grillo/internal/runtimeassets"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/state"

	"golang.org/x/sys/unix"
)

// Check is one doctor result. Status is "ok", "warn", or "fail".
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Why     string `json:"why,omitempty"`
	Inspect string `json:"check,omitempty"`
	Fix     string `json:"fix,omitempty"`
	Docs    string `json:"docs,omitempty"`
}

// DoctorChecks probes the host without changing it and without requiring root.
func DoctorChecks() []Check {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	checks := []Check{
		rootCheck(),
		deviceCheck("/dev/net/tun", "tun"),
		binaryCheck("ip", "ip"),
		binaryCheck("nft", "nft"),
		deviceCheck("/dev/kvm", "kvm"),
		binaryCheck("qemu-system-x86_64", "qemu"),
		virtioFSDCheck(),
		binaryCheck("pasta", "pasta"),
		deviceCheck("/dev/vhost-vsock", "vhost-vsock"),
		userNamespaceCheck(),
		stateDirCheck(),
	}
	checks = append(checks, installedAssetChecks()...)
	checks = append(checks, kvmAPICheck(), actualNamespaceCheck(ctx))
	return append(checks, toolCapabilityChecks(ctx)...)
}

func deviceCheck(path, name string) Check {
	if _, err := os.Stat(path); err != nil {
		return Check{Name: name, Status: "fail", Detail: err.Error()}
	}
	if err := unix.Access(path, unix.R_OK|unix.W_OK); err != nil {
		return Check{Name: name, Status: "fail", Detail: "not readable/writable: " + err.Error()}
	}
	return Check{Name: name, Status: "ok", Detail: path}
}

func binaryCheck(binary, name string) Check {
	path, err := exec.LookPath(binary)
	if err != nil {
		return Check{Name: name, Status: "fail", Detail: binary + " not found in PATH"}
	}
	return Check{Name: name, Status: "ok", Detail: path}
}

func virtioFSDCheck() Check {
	path, err := runtimeassets.VirtioFSD(os.Getenv("GRILLO_VIRTIOFSD_BINARY"))
	if err != nil {
		return Check{Name: "virtiofsd", Status: "fail", Detail: err.Error()}
	}
	return Check{Name: "virtiofsd", Status: "ok", Detail: path}
}

func userNamespaceCheck() Check {
	data, err := os.ReadFile("/proc/sys/user/max_user_namespaces")
	if err != nil {
		return Check{Name: "user-namespaces", Status: "warn", Detail: "cannot read limit"}
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if limit <= 0 {
		return Check{Name: "user-namespaces", Status: "fail", Detail: "disabled"}
	}
	return Check{Name: "user-namespaces", Status: "ok", Detail: strings.TrimSpace(string(data))}
}

func stateDirCheck() Check {
	layout, err := state.NewLayout(state.DefaultConfig())
	if err != nil {
		return Check{Name: "state", Status: "fail", Detail: err.Error()}
	}
	for _, dir := range []string{layout.Runtime, layout.State, layout.Data, layout.Cache} {
		if err := state.CheckPrivateDirectory(dir); err != nil {
			if os.IsNotExist(err) {
				return Check{Name: "state", Status: "warn", Detail: dir + " does not exist yet"}
			}
			return Check{Name: "state", Status: "fail", Detail: err.Error()}
		}
		if err := unix.Access(dir, unix.W_OK); err != nil {
			return Check{Name: "state", Status: "fail", Detail: dir + " is not writable"}
		}
	}
	return Check{Name: "state", Status: "ok", Detail: layout.State}
}

func rootCheck() Check {
	if os.Geteuid() == 0 {
		return Check{Name: "runtime-user", Status: "fail", Detail: "ordinary runtime commands must run as an unprivileged user"}
	}
	return Check{Name: "runtime-user", Status: "ok"}
}

func installedAssetChecks() []Check {
	var checks []Check
	for _, component := range []struct{ name, env string }{{"daemon", "GRILLO_DAEMON_BINARY"}, {"netns", "GRILLO_NETNS_BINARY"}, {"helm", "GRILLO_HELM_BINARY"}} {
		path, err := runtimeassets.Resolve(component.name, os.Getenv(component.env))
		if err == nil {
			err = runtimeassets.Executable(path)
		}
		check := Check{Name: component.name + "-helper", Status: "ok", Detail: path}
		if err != nil {
			check.Status = "fail"
			check.Detail = err.Error()
		}
		checks = append(checks, check)
	}
	path, err := runtimeassets.Resolve("manifest", os.Getenv("GRILLO_GUEST_MANIFEST"))
	if err == nil {
		err = verifyGuestAssets(path, os.Getenv("GRILLO_GUEST_MANIFEST") != "")
	}
	check := Check{Name: "guest-assets", Status: "ok", Detail: "guest inventory integrity verified; installed ABI required, explicit legacy development inventories exempt (not publisher authentication)"}
	if err != nil {
		check.Status = "fail"
		check.Detail = err.Error()
	}
	return append(checks, check)
}

func verifyGuestAssets(path string, allowLegacy bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("guest manifest must be a regular file")
	}
	manifest, err := guest.ReadBootManifest(path, allowLegacy)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		found[artifact.Name] = true
	}
	for _, component := range []struct{ name, env string }{{"kernel", "GRILLO_GUEST_KERNEL"}, {"initramfs", "GRILLO_GUEST_INITRAMFS"}} {
		if !found[component.name] {
			return fmt.Errorf("guest manifest missing %s", component.name)
		}
		wanted, err := runtimeassets.Resolve(component.name, os.Getenv(component.env))
		if err != nil {
			return err
		}
		for _, artifact := range manifest.Artifacts {
			if artifact.Name == component.name {
				actual, err := filepath.Abs(artifact.HostPath())
				if err != nil || actual != wanted {
					return fmt.Errorf("guest %s path does not match manifest", component.name)
				}
			}
		}
	}
	return manifest.Verify()
}

func (a *App) cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	asJSON := fs.Bool("json", false, "machine-readable checks")
	verbose := fs.Bool("verbose", false, "include read-only checks and remediation")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	checks := a.Doctor()
	code := 0
	for i := range checks {
		check := &checks[i]
		if check.Status == "fail" {
			code = 1
		}
		if check.Status != "ok" {
			doctorRemediation(check)
		}
	}
	if *asJSON {
		if err := json.NewEncoder(a.Stdout).Encode(checks); err != nil {
			return 1
		}
	} else {
		PrintDoctor(a.Stdout, checks)
		if *verbose {
			for _, check := range checks {
				if check.Status != "ok" {
					fmt.Fprintf(a.Stdout, "Why: %s\nCheck: %s\nFix: %s\nDocs: %s\n", check.Why, check.Inspect, check.Fix, check.Docs)
				}
			}
		}
	}
	return code
}

// PrintDoctor writes a human-readable report and returns an exit code: 1 if any
// check failed, 0 otherwise.
func PrintDoctor(w io.Writer, checks []Check) int {
	failed := false
	for _, check := range checks {
		status := strings.ToUpper(check.Status)
		fmt.Fprintf(w, "%-4s %-18s %s\n", status, check.Name, check.Detail)
		if check.Status == "fail" {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}
