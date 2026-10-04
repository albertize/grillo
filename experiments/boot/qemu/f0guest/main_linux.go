//go:build linux

// SPDX-License-Identifier: Apache-2.0
// Command f0guest supplies fixed, trusted payloads executed by the spike agent.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/albertize/grillo/experiments/boot/qemu/fsbench"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("missing command")
	}
	switch args[0] {
	case "serve":
		s := &http.Server{Addr: "0.0.0.0:8080", ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 4096, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "f0-vm-server\n") })}
		return s.ListenAndServe()
	case "get":
		if len(args) != 2 {
			return errors.New("get requires URL")
		}
		return get(args[1])
	case "dns":
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, "10.77.0.1:53")
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ips, err := r.LookupHost(ctx, "server.f0.test.")
		if err != nil {
			return err
		}
		if len(ips) != 1 || ips[0] != "10.77.0.11" {
			return fmt.Errorf("unexpected DNS answer: %v", ips)
		}
		fmt.Println("DNS", ips)
		return nil
	case "egress":
		c, err := net.DialTimeout("tcp", "1.1.1.1:443", 5*time.Second)
		if err != nil {
			return err
		}
		return c.Close()
	case "deny":
		if len(args) < 2 {
			return errors.New("deny requires endpoints")
		}
		for _, endpoint := range args[1:] {
			c, err := net.DialTimeout("tcp", endpoint, 500*time.Millisecond)
			if err == nil {
				c.Close()
				return fmt.Errorf("forbidden endpoint reachable: %s", endpoint)
			}
			var ne net.Error
			if !errors.As(err, &ne) {
				return fmt.Errorf("invalid denial test: %w", err)
			}
			fmt.Println("DENIED", endpoint)
		}
		return nil
	case "volume-write", "volume-read":
		if len(args) != 2 {
			return errors.New("volume command requires token")
		}
		if err := os.MkdirAll("/volume", 0700); err != nil {
			return err
		}
		if err := unix.Mount("/dev/vda", "/volume", "ext4", 0, ""); err != nil {
			return err
		}
		operation := func() error {
			p := "/volume/persistent-token"
			if args[0] == "volume-write" {
				f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					return err
				}
				_, err = f.WriteString(args[1])
				if err == nil {
					err = f.Sync()
				}
				err = errors.Join(err, f.Close())
				if err != nil {
					return err
				}
				d, err := os.Open("/volume")
				if err != nil {
					return err
				}
				return errors.Join(d.Sync(), d.Close())
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if string(data) != args[1] {
				return errors.New("persistent token mismatch")
			}
			fmt.Println("PERSISTENCE PASS")
			return nil
		}()
		return errors.Join(operation, unix.Unmount("/volume", 0))
	case "bench":
		if len(args) != 2 {
			return errors.New("bench requires ext4 or virtiofs")
		}
		dir := "/bench"
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		var err error
		switch args[1] {
		case "ext4":
			err = unix.Mount("/dev/vda", dir, "ext4", 0, "")
		case "virtiofs":
			err = unix.Mount("hostshare", dir, "virtiofs", 0, "")
		default:
			return errors.New("unknown bench fs")
		}
		if err != nil {
			return err
		}
		result, err := fsbench.Measure(dir)
		err = errors.Join(err, unix.Unmount(dir, 0))
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	case "share-boundary":
		if err := os.MkdirAll("/share", 0700); err != nil {
			return err
		}
		if err := unix.Mount("hostshare", "/share", "virtiofs", 0, ""); err != nil {
			return err
		}
		defer unix.Unmount("/share", 0)
		// The host fixture symlink points outside the export; the sandbox must not
		// make its target accessible to the guest.
		_, err := os.ReadFile(filepath.Join("/share", "outside-link"))
		if err == nil {
			return errors.New("outside export readable")
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			return err
		}
		fmt.Println("EXPORT BOUNDARY PASS")
		return nil
	default:
		return errors.New("unknown command")
	}
}
func get(url string) error {
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	r, err := c.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return err
	}
	if r.StatusCode != 200 || string(b) != "f0-vm-server\n" {
		return fmt.Errorf("unexpected HTTP response: %d %q", r.StatusCode, b)
	}
	return nil
}
