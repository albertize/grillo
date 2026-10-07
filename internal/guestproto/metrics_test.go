// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"context"
	"testing"
)

func TestMetricsRejectsImpossibleAndAmbiguousSamples(t *testing.T) {
	zero, one := uint64(0), uint64(1)
	for _, result := range []MetricsResult{
		{MemoryTotalBytes: &zero, MemoryAvailableBytes: &one},
		{MemoryAvailableBytes: &one},
		{Containers: []ContainerMetrics{{Name: "app"}, {Name: "app"}}},
		{Containers: make([]ContainerMetrics, 129)},
	} {
		client := newPair(t, HandlerFunc(func(context.Context, Message, *Stream) (any, *Error) { return result, nil }))
		if _, err := client.Metrics(context.Background()); err == nil {
			t.Fatal("invalid sample accepted")
		}
	}
}
func TestMetricsMissingIsNotZero(t *testing.T) {
	client := newPair(t, HandlerFunc(func(context.Context, Message, *Stream) (any, *Error) {
		return MetricsResult{Containers: []ContainerMetrics{{Name: "app"}}}, nil
	}))
	result, err := client.Metrics(context.Background())
	if err != nil || result.MemoryTotalBytes != nil || result.Containers[0].MemoryBytes != nil {
		t.Fatal("missing metrics fabricated")
	}
}
