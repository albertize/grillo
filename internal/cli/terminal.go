//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import "golang.org/x/term"

// Terminal abstracts the local terminal for shell/exec sessions so terminal
// restoration is unit-testable.
type Terminal interface {
	IsTerminal() bool
	// MakeRaw puts the terminal in raw mode and returns a restore function.
	MakeRaw() (restore func(), err error)
}

// realTerminal implements Terminal using the controlling terminal.
type realTerminal struct {
	fd int
}

// NewTerminal returns a Terminal for the given file descriptor (usually stdin).
func NewTerminal(fd int) Terminal { return realTerminal{fd: fd} }

func (t realTerminal) IsTerminal() bool { return term.IsTerminal(t.fd) }
func (t realTerminal) Size() (uint16, uint16) {
	cols, rows, err := term.GetSize(t.fd)
	if err != nil || rows < 1 || cols < 1 {
		return 24, 80
	}
	return uint16(rows), uint16(cols)
}

func (t realTerminal) MakeRaw() (func(), error) {
	state, err := term.MakeRaw(t.fd)
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(t.fd, state) }, nil
}
