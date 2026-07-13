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
		groupA0Labels := labels.FromMap(map[string]string{
			"__address__":       addr,
			"__metrics_path__":  "/metrics/group_a",
			"__param_shard":     "0",
			"scrape_group_name": "group-a",
		})
		groupA1Labels := labels.FromMap(map[string]string{
			"__address__":       addr,
			"__metrics_path__":  "/metrics/group_a",
			"__param_shard":     "1",
			"scrape_group_name": "group-a",
		})
		groupBHFLabels := labels.FromMap(map[string]string{
			"__address__":      addr,
			"__metrics_path__": "/metrics/group_b_hf",
		})
		groupBLFLabels := labels.FromMap(map[string]string{
			"__address__":      addr,
			"__metrics_path__": "/metrics/group_b_lf",
		})
		return []*target.Item{
			target.NewItem("group-a-0", addr, groupA0Labels, "", target.HashLabels(groupA0Labels, "group-a-0")),
			target.NewItem("group-a-1", addr, groupA1Labels, "", target.HashLabels(groupA1Labels, "group-a-1")),
			target.NewItem("group-b-hf", addr, groupBHFLabels, "", target.HashLabels(groupBHFLabels, "group-b-hf")),
			target.NewItem("group-b-lf", addr, groupBLFLabels, "", target.HashLabels(groupBLFLabels, "group-b-lf")),
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
		itemLabels := labels.FromMap(ls)
		return target.NewItem("job", addr, itemLabels, "", target.HashLabels(itemLabels, "job"))
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
