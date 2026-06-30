// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package allocation

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-operator/cmd/otel-allocator/internal/target"
)

func TestConsistentHashingByIdentitySpreadsSameAddress(t *testing.T) {
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
	require.Greater(t, len(seen), 1, "identity hashing must spread same-address targets across collectors")
}
