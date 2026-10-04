//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/albertize/grillo/internal/state"

	"golang.org/x/sys/unix"
)

// Check is one doctor result. Status is "ok", "warn", or "fail".
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// DoctorChecks probes the host without changing it and without requiring root.
func DoctorChecks() []Check {
	return []Check{
		deviceCheck("/dev/kvm", "kvm"),
		binaryCheck("qemu-system-x86_64", "qemu"),
		virtioFSDCheck(),
		binaryCheck("pasta", "pasta"),
		deviceCheck("/dev/vhost-vsock", "vhost-vsock"),
		userNamespaceCheck(),
		stateDirCheck(),
	}
}

func deviceCheck(path, name string) Check {
	if _, err := os.Stat(path); err != nil {
		return Check{Name: name, Status: "fail", Detail: err.Error()}
	}
	if err := unix.Access(path, unix.R_OK|unix.W_OK); err != nil {
		return Check{Name: name, Status: "warn", Detail: "not readable/writable: " + err.Error()}
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
	for _, path := range []string{"/usr/libexec/virtiofsd", "/usr/bin/virtiofsd"} {
		if _, err := os.Stat(path); err == nil {
			return Check{Name: "virtiofsd", Status: "ok", Detail: path}
		}
	}
	if path, err := exec.LookPath("virtiofsd"); err == nil {
		return Check{Name: "virtiofsd", Status: "ok", Detail: path}
	}
	return Check{Name: "virtiofsd", Status: "fail", Detail: "virtiofsd not found"}
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
		if _, err := os.Stat(dir); err != nil {
			return Check{Name: "state", Status: "warn", Detail: dir + " does not exist yet"}
		}
		if err := unix.Access(dir, unix.W_OK); err != nil {
			return Check{Name: "state", Status: "fail", Detail: dir + " is not writable"}
		}
	}
	return Check{Name: "state", Status: "ok", Detail: layout.State}
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
