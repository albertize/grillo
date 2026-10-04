//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0

// Command oci is the T02 host-side OCI scenario harness. It boots the OCI guest
// initramfs (PID 1 + static runc + busybox bundles) and drives runc over the
// vsock exec channel to prove:
//
//	A. OCI image execution: run, exec, signal, delete a container.
//	B. Two containers sharing the guest network namespace reach each other on
//	   localhost (Pod-style co-location).
//
// It is experiment code, not the Grillo runtime or a registry implementation.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/albertize/grillo/experiments/boot/spike"
)

const runcRoot = "/run/runc"

func main() {
	backend := flag.String("backend", "firecracker", "backend: firecracker or qemu")
	fcPath := flag.String("firecracker", "experiments/artifacts/dependencies-v1/bin/firecracker", "firecracker binary")
	kernel := flag.String("kernel", "experiments/artifacts/t02/vmlinux", "Firecracker guest kernel")
	initramfs := flag.String("initramfs", "experiments/artifacts/t02/initramfs-oci.cpio.gz", "OCI guest initramfs")
	qemu := flag.String("qemu", "qemu-system-x86_64", "qemu binary (qemu backend)")
	bzImage := flag.String("bzImage", "experiments/artifacts/qemu/bzImage", "QEMU guest bzImage (qemu backend)")
	vsockCID := flag.Uint("vsock-cid", 3, "guest vsock CID (qemu backend)")
	keep := flag.Bool("keep", false, "print the VMM log on failure")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	var err error
	switch *backend {
	case "firecracker":
		var log *os.File
		if *keep {
			f, cerr := os.CreateTemp("", "grillo-oci-firecracker-*.log")
			if cerr != nil {
				fmt.Fprintln(os.Stderr, "oci: create log:", cerr)
				os.Exit(1)
			}
			defer f.Close()
			log = f
		}
		var sink io.Writer
		if log != nil {
			sink = log
		}
		err = runAll(ctx, spike.Options{Firecracker: *fcPath, Kernel: *kernel, Initramfs: *initramfs, Log: sink})
		if err != nil && log != nil {
			dumpLog(log)
		}
	case "qemu":
		err = runQemu(ctx, *qemu, *bzImage, *initramfs, uint32(*vsockCID), *keep)
	default:
		fmt.Fprintf(os.Stderr, "unknown backend %q (want firecracker or qemu)\n", *backend)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "oci:", err)
		os.Exit(1)
	}
	fmt.Println("RESULT: PASS")
}

// runAll boots the OCI guest via Firecracker and runs both scenarios.
func runAll(ctx context.Context, opts spike.Options) (err error) {
	session, err := spike.Boot(ctx, opts)
	if err != nil {
		return fmt.Errorf("boot: %w", err)
	}
	defer func() { err = errors.Join(err, session.Stop()) }()
	return runScenarios(session)
}

// runScenarios runs both OCI scenarios against a connected session.
func runScenarios(session *spike.Session) error {
	h := &harness{session: session}
	h.scenarioExec()
	h.scenarioLocalhost()
	if h.failed {
		return errors.New("one or more OCI scenario checks failed")
	}
	return nil
}

// runQemu validates the same OCI scenarios on the QEMU backend. QEMU exposes
// vhost-vsock, so the guest agent and harness protocol are unchanged; the host
// connects over AF_VSOCK instead of a Firecracker Unix socket.
func runQemu(ctx context.Context, qemu, bzImage, initramfs string, cid uint32, keep bool) error {
	work, err := os.MkdirTemp("", "grillo-qemu-oci-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	serialLog := filepath.Join(work, "serial.log")
	qemuLog := filepath.Join(work, "qemu.log")

	args := []string{
		"-machine", "microvm,accel=kvm,pcie=off,rtc=on,memory-backend=mem",
		"-cpu", "host", "-smp", "1", "-m", "256",
		"-object", "memory-backend-memfd,id=mem,size=256M,share=on",
		"-no-user-config",
		"-kernel", bzImage,
		"-initrd", initramfs,
		"-append", "console=ttyS0 reboot=k panic=1 rdinit=/init",
		"-device", fmt.Sprintf("vhost-vsock-device,guest-cid=%d", cid),
		"-display", "none",
		"-serial", "file:" + serialLog,
		"-no-reboot",
	}
	logf, err := os.Create(qemuLog)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(qemu, args...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	qemuStart := time.Now()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start qemu: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	kill := func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}

	session, err := spike.Boot(ctx, spike.Options{Kernel: bzImage, Initramfs: initramfs, VsockCID: cid})
	if err != nil {
		kill()
		<-done
		if keep {
			dumpFile(qemuLog)
		}
		return fmt.Errorf("boot: %w", err)
	}
	if rss, ok := qemuRSS(cmd.Process.Pid); ok {
		fmt.Printf("qemu-oci: boot=%s rss=%d KiB\n", time.Since(qemuStart).Round(time.Millisecond), rss)
	} else {
		fmt.Printf("qemu-oci: boot=%s\n", time.Since(qemuStart).Round(time.Millisecond))
	}
	runErr := runScenarios(session)
	stopErr := session.Stop()
	select {
	case exitErr := <-done:
		stopErr = errors.Join(stopErr, exitErr)
	case <-time.After(10 * time.Second):
		kill()
		<-done
		if stopErr == nil {
			stopErr = errors.New("qemu did not exit after stop")
		}
	}
	if runErr != nil {
		if keep {
			dumpFile(qemuLog)
		}
		return runErr
	}
	return stopErr
}

type harness struct {
	session *spike.Session
	failed  bool
}

// runc runs the guest runc with the shared state root.
func (h *harness) runc(args ...string) (stdout, stderr string, code int, err error) {
	return h.session.Exec("/runc", append([]string{"--root", runcRoot}, args...)...)
}

// runcDetached starts a long-lived runc command (e.g. `run -d`) whose children
// must not inherit the management channel.
func (h *harness) runcDetached(args ...string) (stdout, stderr string, code int, err error) {
	return h.session.ExecDetached("/runc", append([]string{"--root", runcRoot}, args...)...)
}

func (h *harness) check(desc string, ok bool, detail string) {
	if ok {
		fmt.Printf("PASS  %s\n", desc)
		return
	}
	h.failed = true
	fmt.Printf("FAIL  %s: %s\n", desc, detail)
}

// scenarioExec proves OCI execution: run, state, exec, signal, delete.
func (h *harness) scenarioExec() {
	fmt.Println("== A. OCI image execution ==")

	out, _, code, err := h.session.Exec("/runc", "--version")
	h.check("guest runc version", err == nil && code == 0 && strings.Contains(out, "runc version"), fmt.Sprintf("code=%d out=%q err=%v", code, out, err))

	_, serr, code, err := h.runcDetached("run", "-d", "--no-pivot", "--bundle", "/oci/bundles/basic", "grillo-basic")
	h.check("start container", err == nil && code == 0, fmt.Sprintf("code=%d stderr=%q err=%v", code, serr, err))

	// The state file is not always visible immediately after a detached start.
	state, detail, running := "", "", false
	for attempt := 1; attempt <= 10; attempt++ {
		out, serr, code, err := h.runc("state", "grillo-basic")
		state, detail = out+serr, fmt.Sprintf("code=%d stderr=%q err=%v", code, serr, err)
		if err == nil && code == 0 && strings.Contains(out, "running") {
			running = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.check("container running", running, fmt.Sprintf("state=%q %s", state, detail))

	out, serr, code, err = h.runc("exec", "grillo-basic", "/bin/echo", "exec-in-container")
	h.check("exec in container", err == nil && code == 0 && strings.Contains(out, "exec-in-container"), fmt.Sprintf("code=%d out=%q stderr=%q err=%v", code, out, serr, err))

	_, serr, code, err = h.runc("kill", "grillo-basic", "TERM")
	h.check("signal container", err == nil && code == 0, fmt.Sprintf("code=%d stderr=%q err=%v", code, serr, err))

	// The container supervisor is reparented to the guest PID 1 and may remain a
	// zombie (this spike does not implement full PID-1 reaping; see the report),
	// so runc may still report it as running. Force-delete is the spike workaround.
	deleted, delDetail := false, ""
	for attempt := 1; attempt <= 10; attempt++ {
		_, serr, code, err := h.runc("delete", "--force", "grillo-basic")
		if err == nil && code == 0 {
			deleted = true
			break
		}
		delDetail = fmt.Sprintf("code=%d stderr=%q err=%v", code, serr, err)
		time.Sleep(200 * time.Millisecond)
	}
	h.check("delete container", deleted, delDetail)
}

// scenarioLocalhost proves two containers in one guest communicate on localhost.
// Both bundles omit a network namespace, so they share the guest network.
func (h *harness) scenarioLocalhost() {
	fmt.Println("== B. Two containers over localhost ==")

	_, serr, code, err := h.runcDetached("run", "-d", "--no-pivot", "--bundle", "/oci/bundles/serve", "grillo-serve")
	h.check("start server container", err == nil && code == 0, fmt.Sprintf("code=%d stderr=%q err=%v", code, serr, err))

	listed, listDetail := false, ""
	for attempt := 1; attempt <= 10; attempt++ {
		list, _, _, _ := h.runc("list")
		listDetail = fmt.Sprintf("list=%q", list)
		if strings.Contains(list, "grillo-serve") {
			listed = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.check("server container listed", listed, listDetail)

	// The client container connects to the server over the shared localhost.
	var clientOut string
	ok := false
	for attempt := 1; attempt <= 10; attempt++ {
		time.Sleep(300 * time.Millisecond)
		out, serr, code, err := h.runc("run", "--no-pivot", "--bundle", "/oci/bundles/client", "grillo-client")
		if err == nil && code == 0 && strings.Contains(out, "grillo-oci-localhost-ok") {
			clientOut, ok = out, true
			break
		}
		clientOut = fmt.Sprintf("attempt %d: code=%d out=%q stderr=%q err=%v", attempt, code, out, serr, err)
	}
	h.check("client reaches server on localhost", ok, clientOut)
	if ok {
		fmt.Printf("      client output: %s", clientOut)
	}

	_, _, _, _ = h.runc("kill", "grillo-serve", "TERM")
	_, _, _, _ = h.runc("delete", "--force", "grillo-client")
	_, _, _, _ = h.runc("delete", "--force", "grillo-serve")
}

// qemuRSS reads the resident set size of the QEMU process from /proc.
func qemuRSS(pid int) (uint64, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func dumpFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "--- %s ---\n%s\n", path, data)
}

func dumpLog(f *os.File) {
	if _, err := f.Seek(0, 0); err != nil {
		return
	}
	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	fmt.Fprintf(os.Stderr, "--- firecracker log ---\n%s\n", buf[:n])
}
