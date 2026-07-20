// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package allocation

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-operator/cmd/otel-allocator/internal/target"
)

// TestConsistentHashingSpreadsSameAddressByStaticLabel is the regression test for the
// 2026-07-20 fleet-ice2 incident: UFM ScrapeConfigs share a host:port and are
// distinguished only by a plain, static `shard` label (not a __param_* or
// __metrics_path__ label - the allocator never sees ScrapeConfig-level metricsPath/
// params/scheme fields at all, since those are merged in by Prometheus's own scrape
// manager, not by the target allocator). Before this fix, GetCollectorForTarget hashed
// on item.TargetURL/__scheme__/__metrics_path__/__param_* alone, so every target here
// hashed identically and landed on one collector.
func TestConsistentHashingSpreadsSameAddressByStaticLabel(t *testing.T) {
	const addr = "10.1.67.190:9002"
	cols := MakeNCollectors(10, 0)

	mkItem := func(shard string) *target.Item {
		ls := labels.FromMap(map[string]string{
			"__address__": addr,
			"shard":       shard,
		})
		jobName := "ufm-mgmt-sys1-ice2-alert-secondary-lf-shard" + shard
		return target.NewItem(jobName, addr, ls, "", target.HashLabels(ls, jobName))
	}

	s := newConsistentHashingStrategy().(*consistentHashingStrategy)
	s.SetCollectors(cols)
	seen := map[string]struct{}{}
	for i := 0; i < 16; i++ {
		item := mkItem(string(rune('0' + i)))
		collector, err := s.GetCollectorForTarget(cols, item)
		require.NoError(t, err)
		seen[collector.Name] = struct{}{}
	}
	require.Greater(t, len(seen), 1, "must spread same-address targets differing only by a static label across collectors")
}

// TestConsistentHashingSpreadsSameAddressByPathOrParam verifies the same spreading
// behavior when targets are distinguished by __metrics_path__ or __param_* labels that
// have already been written into item.Labels (e.g. by a relabel_config), rather than by
// a plain static label.
func TestConsistentHashingSpreadsSameAddressByPathOrParam(t *testing.T) {
	const addr = "10.0.0.1:9001"
	cols := MakeNCollectors(10, 0)

	mkItem := func(jobName, metricsPath, paramShard string) *target.Item {
		ls := labels.FromMap(map[string]string{
			"__address__":      addr,
			"__metrics_path__": metricsPath,
			"__param_shard":    paramShard,
		})
		return target.NewItem(jobName, addr, ls, "", target.HashLabels(ls, jobName))
	}

	items := []*target.Item{
		mkItem("group-a-0", "/metrics/group_a", "0"),
		mkItem("group-a-1", "/metrics/group_a", "1"),
		mkItem("group-b-hf", "/metrics/group_b_hf", ""),
		mkItem("group-b-lf", "/metrics/group_b_lf", ""),
	}

	s := newConsistentHashingStrategy().(*consistentHashingStrategy)
	s.SetCollectors(cols)
	seen := map[string]struct{}{}
	for _, item := range items {
		collector, err := s.GetCollectorForTarget(cols, item)
		require.NoError(t, err)
		seen[collector.Name] = struct{}{}
	}
	require.Greater(t, len(seen), 1, "must spread same-address targets across collectors")
}

// TestConsistentHashingIgnoresMetaLabels verifies the narrower anti-churn guarantee this
// strategy actually provides post-fix: __meta_* labels (raw service-discovery metadata,
// stripped before hashing by target.HashFromBuilder) don't move a target when they
// change. Labels promoted from SD metadata via relabeling (e.g. a plain `netbox_rack`
// label copied from __meta_netbox_rack) are NOT covered by this guarantee - if such a
// value changes for the same address over time, the target will move collectors.
func TestConsistentHashingIgnoresMetaLabels(t *testing.T) {
	const addr = "10.0.0.1:9001"
	cols := MakeNCollectors(10, 0)

	baseLabels := map[string]string{
		"__address__": addr,
		"shard":       "0",
	}
	withMeta := func(value string) *target.Item {
		ls := map[string]string{}
		for k, v := range baseLabels {
			ls[k] = v
		}
		ls["__meta_netbox_rack"] = value
		itemLabels := labels.FromMap(ls)
		return target.NewItem("job", addr, itemLabels, "", target.HashLabels(itemLabels, "job"))
	}

	s := newConsistentHashingStrategy().(*consistentHashingStrategy)
	s.SetCollectors(cols)

	before, err := s.GetCollectorForTarget(cols, withMeta("rack-a1"))
	require.NoError(t, err)
	after, err := s.GetCollectorForTarget(cols, withMeta("rack-b2"))
	require.NoError(t, err)

	require.Equal(t, before.Name, after.Name,
		"must not reassign a target when a __meta_* label changes")
}
