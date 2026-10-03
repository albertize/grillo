//go:build netns

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPastaEgressAndIsolation runs the real rootless helper and checks, from
// inside two concurrent namespaces: egress works, the namespaces are distinct
// and mutually isolated, and a host canary endpoint is unreachable.
//
// Missing pasta, user namespaces, a non-loopback address, or SELinux Enforcing
// is a documented SKIP.
func TestPastaEgressAndIsolation(t *testing.T) {
	if _, err := os.Stat("/proc/self/ns/user"); err != nil {
		t.Skip("SKIP: user namespaces are unavailable")
	}
	if selinuxEnforcing() {
		t.Skip("SKIP: SELinux Enforcing kills pasta helpers on the tested policy")
	}
	hostIP := hostIPv4()
	if hostIP == "" {
		t.Skip("SKIP: no non-loopback IPv4 address")
	}
	canary, err := net.Listen("tcp", net.JoinHostPort(hostIP, "0"))
	if err != nil {
		t.Skipf("SKIP: cannot listen on %s: %v", hostIP, err)
	}
	defer canary.Close()
	hostNS := netNSInode()

	type running struct {
		helper *Helper
		log    *os.File
	}
	var helpers []running
	for i := 0; i < 2; i++ {
		log, err := os.CreateTemp(t.TempDir(), "helper-*.log")
		if err != nil {
			t.Fatal(err)
		}
		env := append(os.Environ(),
			"GRILLO_NET_CHILD=1",
			"GRILLO_MGMT_ADDR="+canary.Addr().String(),
		)
		helper, err := StartHelper(HelperConfig{Log: log, Env: env}, os.Args[0], "-test.run=^TestHelperChild$")
		if err != nil {
			log.Close()
			t.Skipf("SKIP: rootless helper unavailable: %v", err)
		}
		helpers = append(helpers, running{helper: helper, log: log})
	}
	// Both helpers run concurrently, so their namespaces coexist.
	for i, r := range helpers {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		waitErr := r.helper.Wait(ctx, 55*time.Second)
		cancel()
		_ = r.helper.Stop(2 * time.Second)
		_ = r.log.Close()
		if waitErr != nil {
			data, _ := os.ReadFile(r.log.Name())
			t.Fatalf("helper %d failed: %v\n%s", i, waitErr, data)
		}
	}

	type result struct {
		netns, addr           string
		egress, mgmt, isolate bool
	}
	var results []result
	for i, r := range helpers {
		data, _ := os.ReadFile(r.log.Name())
		output := string(data)
		results = append(results, result{
			netns:   field(output, "NETNS "),
			addr:    field(output, "ADDR "),
			egress:  strings.Contains(output, "EGRESS ok"),
			mgmt:    strings.Contains(output, "MGMT reached"),
			isolate: strings.Contains(output, "ISOLATION ok"),
		})
		t.Logf("helper %d: netns=%s addr=%s egress=%v mgmtReached=%v isolate=%v",
			i, results[i].netns, results[i].addr, results[i].egress, results[i].mgmt, results[i].isolate)
	}
	if results[0].netns == hostNS || results[1].netns == hostNS {
		t.Fatal("helper did not create an isolated network namespace")
	}
	if results[0].netns == results[1].netns {
		t.Fatal("two helpers shared a network namespace")
	}
	for i, r := range results {
		if !r.egress {
			t.Errorf("helper %d: guest egress failed", i)
		}
		if r.mgmt {
			t.Errorf("helper %d: host canary endpoint was reachable from the namespace", i)
		}
		if !r.isolate {
			t.Errorf("helper %d: namespaces are not isolated (same port bind failed)", i)
		}
	}
}

// TestHelperChild is the in-namespace probe. It is a no-op outside the helper.
func TestHelperChild(t *testing.T) {
	if os.Getenv("GRILLO_NET_CHILD") != "1" {
		t.Skip("helper child")
	}
	fmt.Printf("NETNS %s\n", netNSInode())
	fmt.Printf("ADDR %s\n", childIPv4())
	// Both helpers bind the same loopback port; success proves separation.
	listener, err := net.Listen("tcp", "127.0.0.1:54321")
	if err != nil {
		fmt.Printf("ISOLATION fail: %v\n", err)
	} else {
		fmt.Println("ISOLATION ok")
		defer listener.Close()
	}
	if conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 5*time.Second); err != nil {
		fmt.Printf("EGRESS fail: %v\n", err)
	} else {
		_ = conn.Close()
		fmt.Println("EGRESS ok")
	}
	if canary := os.Getenv("GRILLO_MGMT_ADDR"); canary != "" {
		if conn, err := net.DialTimeout("tcp", canary, 2*time.Second); err != nil {
			fmt.Println("MGMT denied")
		} else {
			_ = conn.Close()
			fmt.Println("MGMT reached")
		}
	}
}

func netNSInode() string {
	target, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return ""
	}
	return target
}

func hostIPv4() string  { return firstGlobalIPv4() }
func childIPv4() string { return firstGlobalIPv4() }

func firstGlobalIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}

func selinuxEnforcing() bool {
	data, err := os.ReadFile("/sys/fs/selinux/enforce")
	return err == nil && strings.TrimSpace(string(data)) == "1"
}

func field(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
