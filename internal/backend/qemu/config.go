// SPDX-License-Identifier: Apache-2.0

// Package qemu implements the sandbox.Backend contract for the QEMU microvm
// platform selected in ADR 0005. It boots one microVM per sandbox with
// virtiofsd shares, virtio disks, an optional network, and a vhost-vsock control
// channel, and supervises the processes with PID-reuse-safe identity.
package qemu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"grillo.local/grillo/internal/sandbox"
)

// Default paths and limits.
const (
	DefaultQEMU      = "qemu-system-x86_64"
	DefaultVirtioFSD = "/usr/libexec/virtiofsd"
	DefaultVCPU      = 1
	DefaultMemoryMiB = 512
	DefaultCIDBase   = 3
	DefaultBootWait  = 30 * time.Second
)

// Config configures the backend.
type Config struct {
	QEMU        string
	VirtioFSD   string
	Kernel      string
	Initramfs   string
	WorkDir     string
	CIDBase     uint32
	BootTimeout time.Duration
	Logger      func(format string, args ...any)
	// DialGuest connects and handshakes with the guest agent. It defaults to the
	// AF_VSOCK guestproto client. Tests may substitute it.
	DialGuest func(ctx context.Context, spec sandbox.Spec) (GuestConn, error)
}

// GuestConn is the subset of the guest protocol client the backend uses.
type GuestConn interface {
	Status(ctx context.Context) (GuestStatus, error)
	Stop(ctx context.Context) (bool, error)
	Close() error
}

// GuestStatus mirrors the portable subset of the guest status the backend
// exposes in observations.
type GuestStatus struct {
	State      string
	Zombies    int
	Containers int
}

func (c Config) withDefaults() Config {
	out := c
	if out.QEMU == "" {
		out.QEMU = DefaultQEMU
	}
	if out.VirtioFSD == "" {
		out.VirtioFSD = DefaultVirtioFSD
	}
	if out.CIDBase == 0 {
		out.CIDBase = DefaultCIDBase
	}
	if out.BootTimeout == 0 {
		out.BootTimeout = DefaultBootWait
	}
	if out.Logger == nil {
		out.Logger = func(string, ...any) {}
	}
	return out
}

// qemuArgs renders the QEMU command line for a sandbox.
func qemuArgs(spec sandbox.Spec, cfg Config, dir, serialLog string, shareSockets map[string]string) []string {
	vcpu := spec.VCPU
	if vcpu == 0 {
		vcpu = DefaultVCPU
	}
	mem := spec.MemoryMiB
	if mem == 0 {
		mem = DefaultMemoryMiB
	}
	args := []string{
		"-machine", "microvm,accel=kvm,pcie=off,rtc=on,memory-backend=mem",
		"-cpu", "host",
		"-smp", fmt.Sprintf("%d", vcpu),
		"-m", fmt.Sprintf("%d", mem),
		"-object", fmt.Sprintf("memory-backend-memfd,id=mem,size=%dM,share=on", mem),
		"-no-user-config", "-nodefaults", "-monitor", "none",
		"-sandbox", "on,obsolete=deny,elevateprivileges=deny,spawn=deny,resourcecontrol=deny",
		"-kernel", spec.Kernel,
		"-initrd", spec.Initramfs,
		"-append", spec.KernelArgs,
		"-display", "none",
		"-serial", "file:" + serialLog,
		"-no-reboot",
		"-device", fmt.Sprintf("vhost-vsock-device,guest-cid=%d", spec.VsockCID),
	}
	for i, share := range spec.Shares {
		id := fmt.Sprintf("vfsd%d", i)
		args = append(args,
			"-chardev", "socket,id="+id+",path="+shareSockets[share.Tag],
			"-device", "vhost-user-fs-device,chardev="+id+",tag="+share.Tag,
		)
	}
	for _, disk := range spec.Disks {
		format := disk.Format
		if format == "" {
			format = "raw"
		}
		readonly := "off"
		if disk.ReadOnly {
			readonly = "on"
		}
		args = append(args, "-drive", fmt.Sprintf("file=%s,format=%s,if=virtio,readonly=%s", disk.Path, format, readonly))
	}
	if spec.Network != nil && spec.Network.Netdev != "" && spec.Network.Device != "" {
		args = append(args, "-netdev", spec.Network.Netdev, "-device", spec.Network.Device)
	}
	return args
}

// virtiofsdArgs renders the virtiofsd command line for one share.
func virtiofsdArgs(share sandbox.Share, socketPath string) []string {
	args := []string{
		"--socket-path", socketPath,
		"--shared-dir", share.HostPath,
		"--sandbox=namespace",
		"--seccomp=kill",
		"--cache=never",
		"--inode-file-handles=never",
		"--log-level", "warn",
	}
	if share.ReadOnly {
		args = append(args, "--readonly")
	}
	return args
}

func sandboxDir(workDir, id string) string {
	return filepath.Join(workDir, id)
}

func shareSocket(dir, tag string) string {
	return filepath.Join(dir, "vfsd-"+tag+".sock")
}

func serialLogPath(dir string) string { return filepath.Join(dir, "serial.log") }
func vmmLogPath(dir string) string    { return filepath.Join(dir, "vmm.log") }

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("qemu: %s is not a directory", path)
	}
	return os.Chmod(path, 0o700)
}
