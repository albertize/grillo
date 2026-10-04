//go:build linux

// SPDX-License-Identifier: Apache-2.0

// Package network provides rootless-first networking primitives: IPAM with
// persistent owner-bound leases, host port reservations, application isolation,
// loopback publishing, and a supervised pasta helper for guest egress.
package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/state"
)

// Errors returned by the network manager.
var (
	ErrExhausted      = errors.New("network: no free address")
	ErrPortInUse      = errors.New("network: port is already reserved")
	ErrPrivilegedPort = errors.New("network: privileged port")
	ErrNotFound       = errors.New("network: lease not found")
	ErrInvalid        = errors.New("network: invalid configuration")
)

// Lease is a persistent address assignment for one sandbox.
type Lease struct {
	IP          string    `json:"ip"`
	SandboxID   string    `json:"sandboxID"`
	Application string    `json:"application"`
	Operation   string    `json:"operation,omitempty"`
	Network     string    `json:"network,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// IPAM allocates sandbox addresses from one managed subnet. Leases are
// owner-bound and stable for a sandbox's lifetime.
type IPAM struct {
	dir     string
	prefix  netip.Prefix
	gateway netip.Addr

	mu     sync.Mutex
	leases map[string]Lease // by sandbox ID
	byIP   map[string]string
}

type ipamState struct {
	Subnet  string  `json:"subnet"`
	Gateway string  `json:"gateway"`
	Leases  []Lease `json:"leases"`
}

// OpenIPAM opens (or creates) an address pool for a subnet.
func OpenIPAM(dir, subnet, gateway string) (*IPAM, error) {
	prefix, err := netip.ParsePrefix(subnet)
	if err != nil {
		return nil, fmt.Errorf("%w: subnet %q: %v", ErrInvalid, subnet, err)
	}
	prefix = prefix.Masked()
	gw, err := netip.ParseAddr(gateway)
	if err != nil || !prefix.Contains(gw) {
		return nil, fmt.Errorf("%w: gateway %q not in %s", ErrInvalid, gateway, subnet)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ipam := &IPAM{
		dir:     dir,
		prefix:  prefix,
		gateway: gw,
		leases:  map[string]Lease{},
		byIP:    map[string]string{},
	}
	state, err := loadIPAM(dir)
	if err != nil {
		return nil, err
	}
	if state.Subnet != "" && state.Subnet != prefix.String() {
		return nil, fmt.Errorf("%w: stored subnet %s differs from %s", ErrInvalid, state.Subnet, prefix)
	}
	for _, lease := range state.Leases {
		ipam.leases[lease.SandboxID] = lease
		ipam.byIP[lease.IP] = lease.SandboxID
	}
	return ipam, nil
}

// Allocate returns a stable address for a sandbox. Repeating the call for the
// same sandbox is idempotent and returns the existing lease.
func (i *IPAM) Allocate(sandboxID, application, operation, network string) (Lease, error) {
	if sandboxID == "" || application == "" {
		return Lease{}, fmt.Errorf("%w: sandbox and application are required", ErrInvalid)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if lease, ok := i.leases[sandboxID]; ok {
		return lease, nil
	}
	ip, ok := i.freeAddressLocked()
	if !ok {
		return Lease{}, ErrExhausted
	}
	lease := Lease{
		IP:          ip.String(),
		SandboxID:   sandboxID,
		Application: application,
		Operation:   operation,
		Network:     network,
		UpdatedAt:   time.Now().UTC(),
	}
	i.leases[sandboxID] = lease
	i.byIP[lease.IP] = sandboxID
	if err := i.saveLocked(); err != nil {
		delete(i.leases, sandboxID)
		delete(i.byIP, lease.IP)
		return Lease{}, err
	}
	return lease, nil
}

func (i *IPAM) freeAddressLocked() (netip.Addr, bool) {
	broadcast := i.prefix.Addr()
	for {
		next := broadcast.Next()
		if !i.prefix.Contains(next) {
			break
		}
		broadcast = next
	}
	network := i.prefix.Addr()
	for addr := network.Next(); i.prefix.Contains(addr) && addr != broadcast; addr = addr.Next() {
		if addr == i.gateway {
			continue
		}
		if _, used := i.byIP[addr.String()]; used {
			continue
		}
		return addr, true
	}
	return netip.Addr{}, false
}

// Get returns a sandbox's lease.
func (i *IPAM) Get(sandboxID string) (Lease, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	lease, ok := i.leases[sandboxID]
	return lease, ok, nil
}

// LeaseByIP returns the lease for an address.
func (i *IPAM) LeaseByIP(ip string) (Lease, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	id, ok := i.byIP[ip]
	if !ok {
		return Lease{}, false
	}
	lease, ok := i.leases[id]
	return lease, ok
}

// Gateway returns the configured gateway address.
func (i *IPAM) Gateway() string { return i.gateway.String() }

// Subnet returns the managed subnet.
func (i *IPAM) Subnet() string { return i.prefix.String() }

// Release frees a sandbox's lease. Releasing an absent lease succeeds.
func (i *IPAM) Release(sandboxID string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	lease, ok := i.leases[sandboxID]
	if !ok {
		return nil
	}
	delete(i.leases, sandboxID)
	delete(i.byIP, lease.IP)
	return i.saveLocked()
}

// List returns all leases sorted by address.
func (i *IPAM) List() []Lease {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]Lease, 0, len(i.leases))
	for _, lease := range i.leases {
		out = append(out, lease)
	}
	sortLeases(out)
	return out
}

// Reconcile releases leases whose sandbox no longer exists, so a crashed daemon
// never holds addresses for dead sandboxes.
func (i *IPAM) Reconcile(exists func(sandboxID string) bool) ([]string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	var released []string
	for id, lease := range i.leases {
		if exists(id) {
			continue
		}
		delete(i.leases, id)
		delete(i.byIP, lease.IP)
		released = append(released, id)
	}
	if len(released) > 0 {
		if err := i.saveLocked(); err != nil {
			return nil, err
		}
	}
	return released, nil
}

func (i *IPAM) saveLocked() error {
	leases := make([]Lease, 0, len(i.leases))
	for _, lease := range i.leases {
		leases = append(leases, lease)
	}
	sortLeases(leases)
	data, err := json.MarshalIndent(ipamState{Subnet: i.prefix.String(), Gateway: i.gateway.String(), Leases: leases}, "", "  ")
	if err != nil {
		return err
	}
	return state.AtomicWriteFile(i.dir, "leases.json", append(data, '\n'), 0o600, state.DefaultOps())
}

func loadIPAM(dir string) (ipamState, error) {
	data, err := os.ReadFile(filepath.Join(dir, "leases.json"))
	if errors.Is(err, os.ErrNotExist) {
		return ipamState{}, nil
	}
	if err != nil {
		return ipamState{}, err
	}
	var s ipamState
	if err := json.Unmarshal(data, &s); err != nil {
		return ipamState{}, fmt.Errorf("network: decode leases: %w", err)
	}
	return s, nil
}

func sortLeases(leases []Lease) {
	for a := 1; a < len(leases); a++ {
		for b := a; b > 0 && addrLess(leases[b].IP, leases[b-1].IP); b-- {
			leases[b], leases[b-1] = leases[b-1], leases[b]
		}
	}
}

func addrLess(a, b string) bool {
	ai, aerr := netip.ParseAddr(a)
	bi, berr := netip.ParseAddr(b)
	if aerr != nil || berr != nil {
		return a < b
	}
	return ai.Less(bi)
}
