//go:build linux

// SPDX-License-Identifier: Apache-2.0

package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/albertize/grillo/internal/state"
)

// PortReservation is a host loopback port bound to a sandbox target.
type PortReservation struct {
	HostIP      string    `json:"hostIP"`
	HostPort    int       `json:"hostPort"`
	SandboxID   string    `json:"sandboxID"`
	Application string    `json:"application"`
	TargetPort  int       `json:"targetPort"`
	Protocol    string    `json:"protocol"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Ports manages host loopback port reservations so a second `up` cannot steal a
// port from a running application.
type Ports struct {
	dir          string
	mu           sync.Mutex
	reservations map[string]PortReservation // key hostIP:port
}

type portsState struct {
	Reservations []PortReservation `json:"reservations"`
}

// OpenPorts opens (or creates) the reservation store.
func OpenPorts(dir string) (*Ports, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := &Ports{dir: dir, reservations: map[string]PortReservation{}}
	data, err := os.ReadFile(filepath.Join(dir, "reservations.json"))
	if err == nil {
		var s portsState
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("network: decode reservations: %w", err)
		}
		for _, r := range s.Reservations {
			p.reservations[key(r.HostIP, r.HostPort)] = r
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return p, nil
}

// Reserve reserves a host loopback port. Repeating the same reservation for the
// same sandbox is idempotent; a different sandbox is rejected.
func (p *Ports) Reserve(hostIP string, hostPort int, sandboxID, application string, targetPort int, protocol string) (PortReservation, error) {
	if hostPort < 1 || hostPort > 65535 || targetPort < 1 || targetPort > 65535 {
		return PortReservation{}, fmt.Errorf("%w: port out of range", ErrInvalid)
	}
	if !isLoopback(hostIP) {
		return PortReservation{}, fmt.Errorf("%w: host IP %q must be loopback", ErrInvalid, hostIP)
	}
	if hostPort < 1024 {
		return PortReservation{}, fmt.Errorf("%w: port %d needs root; publish %d instead", ErrPrivilegedPort, hostPort, hostPort+10000)
	}
	if protocol == "" {
		protocol = "tcp"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	k := key(hostIP, hostPort)
	if existing, ok := p.reservations[k]; ok {
		if existing.SandboxID != sandboxID {
			return PortReservation{}, fmt.Errorf("%w: %s:%d held by sandbox %s", ErrPortInUse, hostIP, hostPort, existing.SandboxID)
		}
		return existing, nil
	}
	// Detect a port already held by an unrelated host process.
	if err := probeFree(hostIP, hostPort); err != nil {
		return PortReservation{}, err
	}
	reservation := PortReservation{
		HostIP: hostIP, HostPort: hostPort, SandboxID: sandboxID,
		Application: application, TargetPort: targetPort, Protocol: protocol,
		UpdatedAt: time.Now().UTC(),
	}
	p.reservations[k] = reservation
	if err := p.saveLocked(); err != nil {
		delete(p.reservations, k)
		return PortReservation{}, err
	}
	return reservation, nil
}

// Release frees a reservation. Releasing an absent reservation succeeds.
func (p *Ports) Release(hostIP string, hostPort int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := key(hostIP, hostPort)
	if _, ok := p.reservations[k]; !ok {
		return nil
	}
	delete(p.reservations, k)
	return p.saveLocked()
}

// ReleaseSandbox frees every reservation for a sandbox.
func (p *Ports) ReleaseSandbox(sandboxID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := false
	for k, r := range p.reservations {
		if r.SandboxID == sandboxID {
			delete(p.reservations, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return p.saveLocked()
}

// List returns reservations sorted by host address and port.
func (p *Ports) List() []PortReservation {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PortReservation, 0, len(p.reservations))
	for _, r := range p.reservations {
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].HostIP != out[b].HostIP {
			return out[a].HostIP < out[b].HostIP
		}
		return out[a].HostPort < out[b].HostPort
	})
	return out
}

// Reconcile releases reservations for sandboxes that no longer exist.
func (p *Ports) Reconcile(exists func(sandboxID string) bool) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	released := 0
	for k, r := range p.reservations {
		if exists(r.SandboxID) {
			continue
		}
		delete(p.reservations, k)
		released++
	}
	if released > 0 {
		if err := p.saveLocked(); err != nil {
			return 0, err
		}
	}
	return released, nil
}

func (p *Ports) saveLocked() error {
	list := make([]PortReservation, 0, len(p.reservations))
	for _, r := range p.reservations {
		list = append(list, r)
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].HostIP != list[b].HostIP {
			return list[a].HostIP < list[b].HostIP
		}
		return list[a].HostPort < list[b].HostPort
	})
	data, err := json.MarshalIndent(portsState{Reservations: list}, "", "  ")
	if err != nil {
		return err
	}
	return state.AtomicWriteFile(p.dir, "reservations.json", append(data, '\n'), 0o600, state.DefaultOps())
}

func probeFree(hostIP string, hostPort int) error {
	conn, err := net.Listen("tcp", net.JoinHostPort(hostIP, fmt.Sprintf("%d", hostPort)))
	if err != nil {
		return fmt.Errorf("%w: %s:%d: %v", ErrPortInUse, hostIP, hostPort, err)
	}
	return conn.Close()
}

func isLoopback(ip string) bool {
	addr := net.ParseIP(ip)
	return addr != nil && addr.IsLoopback()
}

func key(hostIP string, hostPort int) string {
	return fmt.Sprintf("%s:%d", hostIP, hostPort)
}
