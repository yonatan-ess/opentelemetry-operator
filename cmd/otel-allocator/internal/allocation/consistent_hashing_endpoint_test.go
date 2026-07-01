// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package allocation

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-operator/cmd/otel-allocator/internal/target"
)

// TestConsistentHashingSpreadsSameAddress verifies that this fork's consistent
// hashing distributes targets that share a host:port but differ by metrics path
// or params across multiple collectors, instead of co-locating them.
func TestConsistentHashingSpreadsSameAddress(t *testing.T) {
	const addr = "10.0.0.1:9001"
	cols := MakeNCollectors(10, 0)

	mkItems := func() []*target.Item {
		return []*target.Item{
			target.NewItem("group-a-0", addr, labels.FromMap(map[string]string{
				"__address__":       addr,
				"__metrics_path__":  "/metrics/group_a",
				"__param_shard":     "0",
				"scrape_group_name": "group-a",
			}), ""),
			target.NewItem("group-a-1", addr, labels.FromMap(map[string]string{
				"__address__":       addr,
				"__metrics_path__":  "/metrics/group_a",
				"__param_shard":     "1",
				"scrape_group_name": "group-a",
			}), ""),
			target.NewItem("group-b-hf", addr, labels.FromMap(map[string]string{
				"__address__":      addr,
				"__metrics_path__": "/metrics/group_b_hf",
			}), ""),
			target.NewItem("group-b-lf", addr, labels.FromMap(map[string]string{
				"__address__":      addr,
				"__metrics_path__": "/metrics/group_b_lf",
			}), ""),
		}
	}

	s := newConsistentHashingStrategy().(*consistentHashingStrategy)
	s.SetCollectors(cols)
	seen := map[string]struct{}{}
	for _, item := range mkItems() {
		collector, err := s.GetCollectorForTarget(cols, item)
		require.NoError(t, err)
		seen[collector.Name] = struct{}{}
	}
	require.Greater(t, len(seen), 1, "must spread same-address targets across collectors")
}

// TestConsistentHashingIgnoresNonURLLabels verifies the anti-churn guarantee:
// the hash key is built only from the scrape URL, so changing a label that does
// not affect the URL must not move the target to a different collector.
func TestConsistentHashingIgnoresNonURLLabels(t *testing.T) {
	const addr = "10.0.0.1:9001"
	cols := MakeNCollectors(10, 0)

	urlLabels := map[string]string{
		"__address__":      addr,
		"__scheme__":       "http",
		"__metrics_path__": "/metrics",
		"__param_shard":    "0",
	}
	withExtra := func(name, value string) *target.Item {
		ls := map[string]string{}
		for k, v := range urlLabels {
			ls[k] = v
		}
		ls[name] = value
		return target.NewItem("job", addr, labels.FromMap(ls), "")
	}

	s := newConsistentHashingStrategy().(*consistentHashingStrategy)
	s.SetCollectors(cols)

	// Same scrape URL, different metadata labels (e.g. NetBox metadata that
	// changes over a target's lifetime).
	before, err := s.GetCollectorForTarget(cols, withExtra("netbox_rack", "a1"))
	require.NoError(t, err)
	after, err := s.GetCollectorForTarget(cols, withExtra("netbox_rack", "b2"))
	require.NoError(t, err)

	require.Equal(t, before.Name, after.Name,
		"must not reassign a target when a non-URL label changes")
}
