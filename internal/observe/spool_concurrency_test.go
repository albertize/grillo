//go:build linux

// SPDX-License-Identifier: Apache-2.0

package observe

import (
	"sync"
	"testing"
)

func TestLogSpoolConcurrentProducersAndReaders(t *testing.T) {
	spool, err := OpenLogSpool(t.TempDir(), 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := spool.Append(LogRecord{Resource: "pod", Container: "app", Stream: "stdout", Line: "record"}); err != nil {
					t.Error(err)
				}
				if _, err := spool.List(0, 10, "", ""); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	records, err := spool.List(0, 0, "", "")
	if err != nil || len(records) != 160 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	for i, record := range records {
		if record.Sequence != uint64(i+1) {
			t.Fatal("non-unique sequence")
		}
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := spool.Append(LogRecord{}); err == nil {
		t.Fatal("write after close accepted")
	}
}
