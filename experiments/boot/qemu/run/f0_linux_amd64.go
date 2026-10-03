//go:build linux && amd64

// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"grillo.local/grillo/experiments/boot/qemu/fsbench"
	"grillo.local/grillo/experiments/boot/spike"
)

// All addresses/rules are fixed experiment fixtures, NOT input from manifests.
// They exist only in the fresh user+network namespace owned by pasta.
const f0Rules = `table inet f0 {
 chain input { type filter hook input priority 0; policy drop;
  iifname "lo" accept
  ct state established,related accept
  ip saddr 10.77.0.0/24 udp dport 53 accept
  ip saddr 10.77.0.0/24 tcp dport 53 accept
  counter drop
 }
 chain forward { type filter hook forward priority 0; policy drop;
  ct state established,related accept
  ip saddr 10.77.0.0/24 oifname "uplink" ip daddr 1.1.1.1 tcp dport 443 accept
  counter drop
 }
}
table ip f0nat {
 chain postrouting { type nat hook postrouting priority srcnat; policy accept;
  oifname "uplink" ip saddr 10.77.0.0/24 masquerade
 }
}
`

func command(ctx context.Context, path string, args ...string) error {
	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", path, args, err, output)
	}
	return nil
}

// f0Outer keeps host ports in the original namespace. Publishing crosses only
// an operation-private Unix socket; pasta automatic forwarding is disabled.
func f0Outer(ctx context.Context, opts options) error {
	if os.Getuid() == 0 {
		return errors.New("F0 must run as the unprivileged host user")
	}
	for _, tool := range []string{"pasta", "ip", "nft", "setpriv", "mke2fs", "e2fsck"} {
		if _, err := exec.LookPath(tool); err != nil {
			return err
		}
	}
	if err := selinuxBlocker(); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "grillo-f0-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	share := filepath.Join(work, "share")
	if err = os.Mkdir(share, 0700); err != nil {
		return err
	}
	outside := filepath.Join(work, "outside-canary")
	if err = os.WriteFile(outside, []byte("private-host-canary"), 0600); err != nil {
		return err
	}
	if err = os.Symlink(outside, filepath.Join(share, "outside-link")); err != nil {
		return err
	}
	baseline, err := fsbench.Measure(share)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(baseline)
	fmt.Printf("F0 host-fs %s\n", b)
	volume, err := createVolume(ctx, filepath.Join(work, "volume.ext4"))
	if err != nil {
		return err
	}
	defer volume.Close()
	// The lock stays held across BOTH VM boots. A second attachment is rejected.
	if other, err := lockVolume(volume.Name()); err == nil {
		other.Close()
		return errors.New("second writable volume attachment accepted")
	}
	fmt.Println("F0 volume-exclusive-lock PASS")
	pub, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer pub.Close()
	stopRelay := relay(ctx, pub, func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(work, "publish.sock"))
	})
	defer stopRelay()
	// A real live host-only service: positive host access, negative guest access.
	canary, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer canary.Close()
	canaryPort := fmt.Sprint(canary.Addr().(*net.TCPAddr).Port)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	kernel, err := filepath.Abs(opts.Kernel)
	if err != nil {
		return err
	}
	initrd, err := filepath.Abs(opts.Initramfs)
	if err != nil {
		return err
	}
	logfile, err := os.Create(filepath.Join(work, "inner.log"))
	if err != nil {
		return err
	}
	defer logfile.Close()
	p, err := startProcess2("pasta", logfile, "-f", "-4", "-I", "uplink", "--config-net", "--no-map-gw", "-t", "none", "-u", "none", "-T", "none", "-U", "none", "--", exe, "-scenario", "f0-inner", "-work", work, "-kernel", kernel, "-initramfs", initrd, "-qemu", opts.QEMU, "-virtiofsd", opts.VirtioFSD, "-canary-port", canaryPort, "-timeout", "10m")
	if err != nil {
		return err
	}
	defer p.stop(io.Discard)
	defer func() { data, _ := os.ReadFile(logfile.Name()); fmt.Print(string(data)) }()
	if err = waitProcessPath(ctx, p, filepath.Join(work, "publish-ready"), 90*time.Second); err != nil {
		return err
	}
	if err = checkHTTP(ctx, "http://"+pub.Addr().String()); err != nil {
		return fmt.Errorf("host publish: %w", err)
	}
	// Host publishing is reachable on loopback only; guest access to management
	// is tested separately from this explicit application endpoint.
	fmt.Println("F0 host-loopback-publish PASS", pub.Addr())
	if err = os.WriteFile(filepath.Join(work, "publish-ok"), []byte("ok"), 0600); err != nil {
		return err
	}
	if err = p.wait(ctx, 10*time.Minute); err != nil {
		return err
	}
	if err = command(ctx, "e2fsck", "-fn", volume.Name()); err != nil {
		return err
	}
	fmt.Println("F0 ext4-offline-check PASS")
	return nil
}

func waitProcessPath(ctx context.Context, p *managed, path string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		select {
		case err := <-p.done:
			p.finished = true
			return fmt.Errorf("helper exited before readiness: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
			if _, err := os.Stat(path); err == nil {
				return nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
}

// selinuxBlocker surfaces a measured distribution restriction instead of a
// mysterious timeout: with SELinux Enforcing, the tested Fedora 44 policy
// silently terminates helper processes started inside the pasta user namespace
// (exit code 2, empty output). This is a host prerequisite for doctor to
// explain, never a reason to escalate privileges automatically.
func selinuxBlocker() error {
	data, err := os.ReadFile("/sys/fs/selinux/enforce")
	if err != nil {
		return nil // No SELinux on this host: nothing to check.
	}
	if strings.TrimSpace(string(data)) != "1" {
		return nil
	}
	return errors.New("SELinux is Enforcing; on the tested Fedora policy helper processes in the pasta user namespace are silently killed (exit 2, no output). Temporarily set it to Permissive (for example 'sudo setenforce 0') or provide a policy, then rerun")
}

func createVolume(ctx context.Context, path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Truncate(128 << 20); err == nil {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	}
	if err == nil {
		err = command(ctx, "mke2fs", "-q", "-t", "ext4", "-F", path)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func lockVolume(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func setupTopology(ctx context.Context) error {
	if os.Getuid() != 0 {
		return errors.New("inner helper requires namespace-mapped root")
	}
	mapping, err := os.ReadFile("/proc/self/uid_map")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(mapping))
	if len(fields) != 3 || fields[0] != "0" || fields[1] == "0" || fields[2] != "1" {
		return fmt.Errorf("unsafe user namespace mapping: %q", mapping)
	}
	fmt.Printf("F0 uid-map %s", mapping)
	status, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Cap") || strings.HasPrefix(line, "NoNew") || strings.HasPrefix(line, "Seccomp") {
			fmt.Println("F0 helper", line)
		}
	}
	for _, a := range [][]string{
		{"link", "set", "lo", "up"},
		{"link", "add", "br0", "type", "bridge"}, {"addr", "add", "10.77.0.1/24", "dev", "br0"}, {"link", "set", "br0", "up"},
		{"link", "add", "br1", "type", "bridge"}, {"addr", "add", "10.78.0.1/24", "dev", "br1"}, {"link", "set", "br1", "up"},
	} {
		if err := command(ctx, "ip", a...); err != nil {
			return err
		}
	}
	for i := 0; i < 3; i++ {
		tap := fmt.Sprintf("tap%d", i)
		bridge := "br0"
		if i == 2 {
			bridge = "br1"
		}
		for _, a := range [][]string{{"tuntap", "add", "dev", tap, "mode", "tap", "user", "0"}, {"link", "set", tap, "master", bridge}, {"link", "set", tap, "up"}} {
			if err := command(ctx, "ip", a...); err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(f0Rules)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("namespace firewall: %w: %s", err, b)
	}
	return nil
}

type f0VM struct {
	process *managed
	session *spike.Session
	boot    time.Duration
}

func bootF0(ctx context.Context, opts options, work string, index int, cid uint32, withShare bool) (*f0VM, error) {
	ip := "10.77.0.11"
	gateway := "10.77.0.1"
	if index == 1 {
		ip = "10.77.0.12"
	}
	if index == 2 {
		ip = "10.78.0.11"
		gateway = "10.78.0.1"
	}
	serial := filepath.Join(work, fmt.Sprintf("vm%d-serial.log", index))
	// No secret is passed on the kernel command line or argv: this spike does
	// not authenticate the channel (that is T06).
	args := baseQEMUArgs(opts, serial, bootKernelArg+" ip="+ip+"::"+gateway+":255.255.255.0::eth0:off")
	args = append(args, "-netdev", fmt.Sprintf("tap,id=n0,ifname=tap%d,script=no,downscript=no", index), "-device", fmt.Sprintf("virtio-net-device,netdev=n0,mac=02:00:00:00:00:%02x", index+1), "-device", fmt.Sprintf("vhost-vsock-device,guest-cid=%d", cid))
	if index == 0 {
		args = append(args, "-drive", "if=none,id=vol,format=raw,file="+filepath.Join(work, "volume.ext4"), "-device", "virtio-blk-device,drive=vol")
		if withShare {
			args = append(args, "-chardev", "socket,id=fs,path="+filepath.Join(work, "vfsd.sock"), "-device", "vhost-user-fs-device,chardev=fs,tag=hostshare")
		}
	}
	f, err := os.Create(filepath.Join(work, fmt.Sprintf("vm%d.log", index)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	start := time.Now()
	// TAP owner is mapped UID 0; QEMU needs no namespace capabilities after setup.
	drop := []string{"--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs", opts.QEMU}
	p, err := startProcess2("setpriv", f, append(drop, args...)...)
	if err != nil {
		return nil, err
	}
	s, err := spike.Boot(ctx, spike.Options{VsockCID: cid, BootTimeout: 15 * time.Second, ExecTimeout: 90 * time.Second})
	if err != nil {
		p.stop(io.Discard)
		qlog, _ := os.ReadFile(f.Name())
		return nil, fmt.Errorf("boot VM%d: %w: %s", index, err, qlog)
	}
	vm := &f0VM{process: p, session: s, boot: time.Since(start)}
	return vm, nil
}
func (v *f0VM) stop(ctx context.Context) error {
	err := v.session.Stop()
	return errors.Join(err, v.process.wait(ctx, 10*time.Second))
}
func (v *f0VM) close() { _ = v.session.Close(); v.process.stop(io.Discard) }
func guest(v *f0VM, args ...string) error {
	out, stderr, code, err := v.session.Exec("/probe", args...)
	if err != nil || code != 0 {
		return fmt.Errorf("guest %v: code=%d: %w: %s%s", args, code, err, out, stderr)
	}
	fmt.Printf("F0 guest %s PASS %s", args[0], out)
	if out == "" {
		fmt.Println()
	}
	return nil
}
func hardening(pid int) error {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return err
	}
	want := map[string]string{"NoNewPrivs:": "1", "Seccomp:": "2", "CapEff:": "0000000000000000"}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			if value, ok := want[f[0]]; ok {
				if f[1] != value {
					return fmt.Errorf("hardening %s got %s", f[0], f[1])
				}
				delete(want, f[0])
				fmt.Println("F0 qemu", line)
			}
			if f[0] == "VmRSS:" {
				fmt.Println("F0 qemu", line)
			}
		}
	}
	if len(want) != 0 {
		return fmt.Errorf("missing hardening fields: %v", want)
	}
	b, err = os.ReadFile(fmt.Sprintf("/proc/%d/smaps_rollup", pid))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "Pss:") {
			fmt.Println("F0 qemu", line)
		}
	}
	return nil
}

func f0Inner(ctx context.Context, opts options, work, canaryPort string) (err error) {
	opts = opts.withDefaults()
	if err = setupTopology(ctx); err != nil {
		return err
	}
	stopDNS, err := startFixtureDNS(ctx)
	if err != nil {
		return err
	}
	defer stopDNS()
	sock, err := net.Listen("unix", filepath.Join(work, "publish.sock"))
	if err != nil {
		return err
	}
	defer sock.Close()
	stopRelay := relay(ctx, sock, func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", "10.77.0.11:8080")
	})
	defer stopRelay()
	vfslog, err := os.Create(filepath.Join(work, "vfsd.log"))
	if err != nil {
		return err
	}
	defer vfslog.Close()
	vfs, err := startProcess2(opts.VirtioFSD, vfslog, "--socket-path", filepath.Join(work, "vfsd.sock"), "--shared-dir", filepath.Join(work, "share"), "--sandbox=namespace", "--seccomp=kill", "--cache=never", "--inode-file-handles=never")
	if err != nil {
		return err
	}
	defer vfs.stop(io.Discard)
	if err = waitForPath(ctx, filepath.Join(work, "vfsd.sock"), 10*time.Second); err != nil {
		return err
	}
	var seed [8]byte
	if _, err = rand.Read(seed[:]); err != nil {
		return err
	}
	cid := 1024 + binary.LittleEndian.Uint32(seed[:4])%1000000000
	token := hex.EncodeToString(seed[:])
	var vms []*f0VM
	defer func() {
		for _, vm := range vms {
			vm.close()
		}
	}()
	for i := 0; i < 3; i++ {
		vm, e := bootF0(ctx, opts, work, i, cid+uint32(i), i == 0)
		if e != nil {
			return e
		}
		vms = append(vms, vm)
		if e = hardening(vm.process.cmd.Process.Pid); e != nil {
			return e
		}
	}
	for _, i := range []int{0, 2} {
		_, _, code, e := vms[i].session.ExecDetached("/probe", "serve")
		if e != nil || code != 0 {
			return fmt.Errorf("start HTTP server: code=%d err=%v", code, e)
		}
	}
	for _, address := range []string{"10.77.0.11", "10.78.0.11"} {
		if err = waitHTTP(ctx, "http://"+address+":8080"); err != nil {
			return err
		}
	}
	if err = guest(vms[1], "get", "http://10.77.0.11:8080"); err != nil {
		return err
	}
	if err = guest(vms[1], "dns"); err != nil {
		return err
	}
	if err = guest(vms[1], "egress"); err != nil {
		return err
	}
	if err = guest(vms[1], "deny", "10.78.0.11:8080", "10.77.0.11:1024", "10.77.0.1:1024", "10.77.0.1:"+canaryPort, "10.78.0.1:53"); err != nil {
		return err
	}
	if err = guest(vms[2], "deny", "10.77.0.11:8080", "10.77.0.1:53"); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(work, "publish-ready"), []byte("ready"), 0600); err != nil {
		return err
	}
	if err = waitForPath(ctx, filepath.Join(work, "publish-ok"), 30*time.Second); err != nil {
		return err
	}
	if err = guest(vms[0], "share-boundary"); err != nil {
		return err
	}
	if err = guest(vms[0], "bench", "virtiofs"); err != nil {
		return err
	}
	if err = guest(vms[0], "bench", "ext4"); err != nil {
		return err
	}
	if err = guest(vms[0], "volume-write", token); err != nil {
		return err
	}
	if err = vms[0].stop(ctx); err != nil {
		return err
	}
	// vhost-user fs reconnects to the same daemon; persistence reuses only the
	// disk image, not guest RAM or a snapshot.
	replacement, err := bootF0(ctx, opts, work, 0, cid, false)
	if err != nil {
		return err
	}
	vms = append(vms, replacement)
	if err = guest(replacement, "volume-read", token); err != nil {
		return err
	}
	if err = replacement.stop(ctx); err != nil {
		return err
	}
	// Warm boot baseline: identical simple guest, one TAP and vsock, no workload.
	if err = vms[1].stop(ctx); err != nil {
		return err
	}
	var samples []int64
	for i := 0; i < 30; i++ {
		vm, e := bootF0(ctx, opts, work, 1, cid+1, false)
		if e != nil {
			return e
		}
		samples = append(samples, vm.boot.Microseconds())
		e = vm.stop(ctx)
		vm.close()
		if e != nil {
			return e
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	fmt.Printf("F0 warm-boot samples=30 failures=0 median_us=%d p95_us=%d\n", samples[15], samples[28])
	if err = vms[2].stop(ctx); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "nft", "list", "ruleset")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	fmt.Printf("F0 firewall evidence\n%s", b)
	fmt.Println("F0 inner PASS")
	return nil
}

func checkHTTP(ctx context.Context, url string) error {
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	c := &http.Client{Timeout: 3 * time.Second, Transport: tr}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	r, err := c.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return err
	}
	if r.StatusCode != 200 || string(data) != "f0-vm-server\n" {
		return fmt.Errorf("unexpected response %d %q", r.StatusCode, data)
	}
	return nil
}
func waitHTTP(ctx context.Context, url string) error {
	var err error
	for i := 0; i < 30; i++ {
		if err = checkHTTP(ctx, url); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// relay is deliberately bounded and only exposes a single fixed fixture peer.
func relay(parent context.Context, ln net.Listener, dial func(context.Context) (net.Conn, error)) func() {
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				c.Close()
				return
			default:
				c.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				d, err := dial(ctx)
				if err != nil {
					return
				}
				defer d.Close()
				d.SetDeadline(time.Now().Add(5 * time.Second))
				done := make(chan struct{})
				go func() {
					io.Copy(d, io.LimitReader(c, 1<<20))
					if tcp, ok := d.(interface{ CloseWrite() error }); ok {
						tcp.CloseWrite()
					}
					close(done)
				}()
				io.Copy(c, io.LimitReader(d, 1<<20))
				d.Close()
				c.Close()
				<-done
			}()
		}
	}()
	return func() { cancel(); ln.Close(); wg.Wait() }
}
