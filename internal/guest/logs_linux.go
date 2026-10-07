//go:build linux

// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"io"
	"sync"

	"github.com/albertize/grillo/internal/guestproto"
)

// ContainerLogs is a guest-wide bounded ring. Writers never wait for a host
// reader or touch disk. Eviction is reported on cursor reads, not concealed.
// Payload retention is at most 256 * 4096 bytes, plus bounded record metadata.
type ContainerLogs struct {
	mu         sync.Mutex
	records    [256]guestproto.LogChunk
	head, size int
	next       uint64
}

type containerLogWriter struct {
	logs              *ContainerLogs
	container, stream string
}

func (l *ContainerLogs) Writer(container, stream string) io.Writer {
	return containerLogWriter{l, container, stream}
}

func (w containerLogWriter) Write(data []byte) (int, error) {
	n := len(data)
	for len(data) > 0 {
		size := min(len(data), guestproto.MaxLogChunkBytes)
		chunk := append([]byte(nil), data[:size]...)
		w.logs.mu.Lock()
		w.logs.next++
		record := guestproto.LogChunk{Sequence: w.logs.next, Container: w.container, Stream: w.stream, Data: chunk}
		if w.logs.size == len(w.logs.records) {
			w.logs.records[w.logs.head] = record
			w.logs.head = (w.logs.head + 1) % len(w.logs.records)
		} else {
			w.logs.records[(w.logs.head+w.logs.size)%len(w.logs.records)] = record
			w.logs.size++
		}
		w.logs.mu.Unlock()
		data = data[size:]
	}
	return n, nil
}

func (l *ContainerLogs) Read(after uint64) guestproto.LogsResult {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := guestproto.LogsResult{Next: after}
	if l.size == 0 {
		return result
	}
	oldest := l.records[l.head].Sequence
	result.Gap = after < oldest-1
	for i := 0; i < l.size; i++ {
		record := l.records[(l.head+i)%len(l.records)]
		if record.Sequence <= after {
			continue
		}
		// Copy bytes so callers cannot mutate retained data.
		record.Data = append([]byte(nil), record.Data...)
		result.Chunks = append(result.Chunks, record)
		result.Next = record.Sequence
		if len(result.Chunks) == guestproto.MaxLogBatchChunks {
			break
		}
	}
	return result
}
