//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0

// Command run is the T03/QEMU live-share harness. It starts virtiofsd on a host
// directory, boots a QEMU microvm with a vhost-user-fs device, and verifies that
// (a) the guest sees host writes, (b) host sees guest writes and renames live,
// and (c) a read-only mount rejects writes. It is experiment code, not the
// Grillo runtime.
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
	"strings"
	"syscall"
	"time"
)

const (
	sentinelName  = "host-sentinel.txt"
	sentinelData  = "host-sentinel-v1\n"
	guestPayload  = "guest-payload\n"
	virtiofsTag   = "hostshare"
	guestRenamed  = "guest-renamed.txt"
	bootKernelArg = "console=ttyS0 reboot=k panic=1 rdinit=/init"
)

// options configures one live-share experiment.
type options struct {
	QEMU        string
	VirtioFSD   string
	Kernel      string
	Initramfs   string
	WorkDir     string
	MemMiB      int
	BootTimeout time.Duration
	Log         io.Writer
}

func (o options) withDefaults() options {
	out := o
	if out.QEMU == "" {
		out.QEMU = "qemu-system-x86_64"
	}
	if out.MemMiB == 0 {
		out.MemMiB = 256
	}
	if out.BootTimeout == 0 {
		out.BootTimeout = 60 * time.Second
	}
	if out.Log == nil {
		out.Log = io.Discard
	}
	return out
}

// runProbe performs one experiment and returns the guest serial console output.
func runProbe(ctx context.Context, raw options) (string, error) {
	opts := raw.withDefaults()
	work, err := os.MkdirTemp(opts.WorkDir, "grillo-qemu-")
	if err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}
	if opts.Log == io.Discard {
		defer os.RemoveAll(work)
	} else {
		fmt.Fprintln(opts.Log, "work dir:", work)
	}

	share := filepath.Join(work, "share")
	if err := os.MkdirAll(share, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(share, sentinelName), []byte(sentinelData), 0o644); err != nil {
		return "", err
	}

	vfsdSock := filepath.Join(work, "vfsd.sock")
	serialLog := filepath.Join(work, "serial.log")
	qemuLog := filepath.Join(work, "qemu.log")

	vfsd, err := startProcess(opts.VirtioFSD,
		"--socket-path", vfsdSock,
		"--shared-dir", share,
		"--sandbox=none",
		"--log-level", "warn",
	)
	if err != nil {
		return "", fmt.Errorf("start virtiofsd: %w", err)
	}
	defer vfsd.stop(opts.Log)

	if err := waitForPath(vfsdSock, 10*time.Second); err != nil {
		return "", fmt.Errorf("virtiofsd socket: %w", err)
	}

	qemuArgs := baseQEMUArgs(opts, serialLog, bootKernelArg)
	qemuArgs = append(qemuArgs,
		"-chardev", "socket,id=vfsd,path="+vfsdSock,
		"-device", "vhost-user-fs-device,chardev=vfsd,tag="+virtiofsTag,
	)
	qemuLogFile, err := os.Create(qemuLog)
	if err != nil {
		return "", err
	}
	defer qemuLogFile.Close()
	qemu, err := startProcess2(opts.QEMU, qemuLogFile, qemuArgs...)
	if err != nil {
		return "", fmt.Errorf("start qemu: %w", err)
	}

	waitErr := qemu.wait(opts.BootTimeout)

	serial, _ := os.ReadFile(serialLog)
	var problems []string
	if !strings.Contains(string(serial), "STORAGE RESULT: PASS") {
		problems = append(problems, "guest did not report STORAGE RESULT: PASS")
	}
	data, rerr := os.ReadFile(filepath.Join(share, guestRenamed))
	switch {
	case rerr != nil:
		problems = append(problems, "host missing "+guestRenamed+": "+rerr.Error())
	case string(data) != guestPayload:
		problems = append(problems, fmt.Sprintf("%s content = %q, want %q", guestRenamed, data, guestPayload))
	}
	if waitErr != nil {
		qlog, _ := os.ReadFile(qemuLog)
		problems = append(problems, fmt.Sprintf("qemu: %v; log: %s", waitErr, strings.TrimSpace(string(qlog))))
	}
	if len(problems) > 0 {
		return string(serial), errors.New(strings.Join(problems, "; "))
	}
	return string(serial), nil
}

// baseQEMUArgs builds the QEMU microvm arguments shared by all scenarios.
func baseQEMUArgs(opts options, serialLog, appendArgs string) []string {
	memSize := fmt.Sprintf("%dM", opts.MemMiB)
	return []string{
		"-machine", "microvm,accel=kvm,pcie=off,rtc=on,memory-backend=mem",
		"-cpu", "host",
		"-smp", "1",
		"-m", fmt.Sprintf("%d", opts.MemMiB),
		"-object", "memory-backend-memfd,id=mem,size=" + memSize + ",share=on",
		"-no-user-config",
		"-kernel", opts.Kernel,
		"-initrd", opts.Initramfs,
		"-append", appendArgs,
		"-display", "none",
		"-serial", "file:" + serialLog,
		"-no-reboot",
	}
}

// runNet boots QEMU with rootless user-mode networking (no host TAP or sudo) and
// verifies guest TCP egress and DNS. The kernel cmdline `ip=dhcp` configures the
// interface from QEMU's built-in DHCP server.
func runNet(ctx context.Context, raw options) (string, error) {
	opts := raw.withDefaults()
	work, err := os.MkdirTemp(opts.WorkDir, "grillo-qemu-net-")
	if err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}
	if opts.Log == io.Discard {
		defer os.RemoveAll(work)
	} else {
		fmt.Fprintln(opts.Log, "work dir:", work)
	}

	serialLog := filepath.Join(work, "serial.log")
	qemuLog := filepath.Join(work, "qemu.log")
	qemuArgs := baseQEMUArgs(opts, serialLog, bootKernelArg+" ip=dhcp")
	qemuArgs = append(qemuArgs,
		"-netdev", "user,id=n0",
		"-device", "virtio-net-device,netdev=n0",
	)

	qemuLogFile, err := os.Create(qemuLog)
	if err != nil {
		return "", err
	}
	defer qemuLogFile.Close()
	qemu, err := startProcess2(opts.QEMU, qemuLogFile, qemuArgs...)
	if err != nil {
		return "", fmt.Errorf("start qemu: %w", err)
	}
	waitErr := qemu.wait(opts.BootTimeout)

	serial, _ := os.ReadFile(serialLog)
	if !strings.Contains(string(serial), "NET RESULT: PASS") {
		detail := "guest did not report NET RESULT: PASS"
		if waitErr != nil {
			qlog, _ := os.ReadFile(qemuLog)
			detail += fmt.Sprintf("; qemu: %v; log: %s", waitErr, strings.TrimSpace(string(qlog)))
		}
		return string(serial), errors.New(detail)
	}
	return string(serial), nil
}

// managed wraps a helper process with a process group and parent-death signal.
type managed struct {
	cmd  *exec.Cmd
	done chan error
}

func startProcess(path string, args ...string) (*managed, error) {
	return startProcess2(path, nil, args...)
}

func startProcess2(path string, stdout *os.File, args ...string) (*managed, error) {
	cmd := exec.Command(path, args...)
	if stdout != nil {
		cmd.Stdout = stdout
		cmd.Stderr = stdout
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	m := &managed{cmd: cmd, done: make(chan error, 1)}
	go func() { m.done <- cmd.Wait() }()
	return m, nil
}

func (m *managed) wait(timeout time.Duration) error {
	select {
	case err := <-m.done:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return err
		}
		return err
	case <-time.After(timeout):
		m.kill()
		<-m.done
		return fmt.Errorf("process did not exit within %s", timeout)
	}
}

func (m *managed) stop(log io.Writer) {
	if m.cmd.Process != nil {
		_ = syscall.Kill(-m.cmd.Process.Pid, syscall.SIGKILL)
	}
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
	}
}

func (m *managed) kill() {
	if m.cmd.Process != nil {
		_ = syscall.Kill(-m.cmd.Process.Pid, syscall.SIGKILL)
	}
}

func waitForPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func main() {
	var (
		qemu      = flag.String("qemu", "qemu-system-x86_64", "qemu binary")
		virtiofsd = flag.String("virtiofsd", "/usr/libexec/virtiofsd", "virtiofsd binary")
		kernel    = flag.String("kernel", "experiments/artifacts/qemu/bzImage", "guest bzImage with CONFIG_VIRTIO_FS")
		initramfs = flag.String("initramfs", "experiments/artifacts/qemu/initramfs-probe.cpio.gz", "probe initramfs")
		scenario  = flag.String("scenario", "share", "share (virtiofs) or net (user-mode networking)")
		verbose   = flag.Bool("verbose", false, "print the guest serial console")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	opts := options{
		QEMU:      *qemu,
		VirtioFSD: *virtiofsd,
		Kernel:    *kernel,
		Initramfs: *initramfs,
	}
	var serial string
	var err error
	switch *scenario {
	case "share":
		serial, err = runProbe(ctx, opts)
	case "net":
		serial, err = runNet(ctx, opts)
	default:
		fmt.Fprintf(os.Stderr, "unknown scenario %q (want share or net)\n", *scenario)
		os.Exit(2)
	}
	if *verbose {
		fmt.Print(serial)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "qemu", *scenario, "FAIL:", err)
		os.Exit(1)
	}
	fmt.Printf("RESULT: PASS (%s)\n", *scenario)
	if !*verbose {
		fmt.Print(serial)
	}
}
