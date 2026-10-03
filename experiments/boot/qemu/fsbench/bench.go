// SPDX-License-Identifier: Apache-2.0

// Package fsbench is a small, fixed warm-cache filesystem experiment, not a
// general benchmark suite. Every sample owns and removes only its temp tree.
package fsbench

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const Files = 128
const Samples = 30

type Timing struct {
	MedianUS int64 `json:"median_us"`
	P95US    int64 `json:"p95_us"`
}
type Result struct {
	Files        int               `json:"files"`
	BytesPerFile int               `json:"bytes_per_file"`
	Samples      int               `json:"samples"`
	Phases       map[string]Timing `json:"phases"`
}

func Measure(parent string) (Result, error) {
	times := make(map[string][]int64)
	for sample := 0; sample < Samples; sample++ {
		if err := one(parent, times); err != nil {
			return Result{}, err
		}
	}
	r := Result{Files: Files, BytesPerFile: 4096, Samples: Samples, Phases: map[string]Timing{}}
	for phase, v := range times {
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		r.Phases[phase] = Timing{v[len(v)/2], v[(len(v)*95+99)/100-1]}
	}
	return r, nil
}
func one(parent string, times map[string][]int64) error {
	dir, err := os.MkdirTemp(parent, "f0-bench-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	payload := bytes.Repeat([]byte("x"), 4096)
	paths := make([]string, Files)
	for i := range paths {
		paths[i] = filepath.Join(dir, fmt.Sprintf("f-%03d", i))
	}
	phase := func(name string, fn func() error) error {
		start := time.Now()
		if err := fn(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		times[name] = append(times[name], time.Since(start).Microseconds())
		return nil
	}
	if err = phase("write_fsync", func() error {
		for _, p := range paths {
			f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			_, e = f.Write(payload)
			if e == nil {
				e = f.Sync()
			}
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err = phase("read", func() error {
		for _, p := range paths {
			data, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			if !bytes.Equal(data, payload) {
				return fmt.Errorf("content mismatch")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err = phase("rename", func() error {
		for i, p := range paths {
			if e := os.Rename(p, p+".new"); e != nil {
				return e
			}
			paths[i] = p + ".new"
		}
		return nil
	}); err != nil {
		return err
	}
	if err = phase("chmod", func() error {
		for _, p := range paths {
			if e := os.Chmod(p, 0640); e != nil {
				return e
			}
			s, e := os.Stat(p)
			if e != nil {
				return e
			}
			if s.Mode().Perm() != 0640 {
				return fmt.Errorf("mode mismatch")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return phase("remove", func() error {
		for _, p := range paths {
			if e := os.Remove(p); e != nil {
				return e
			}
		}
		return nil
	})
}
