// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package allocation

import (
	"fmt"

	"github.com/buraksezer/consistent"
	"github.com/cespare/xxhash/v2"

	"github.com/open-telemetry/opentelemetry-operator/cmd/otel-allocator/internal/target"
)

const consistentHashingStrategyName = "consistent-hashing"

type hasher struct{}

func (hasher) Sum64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

var _ Strategy = &consistentHashingStrategy{}

type consistentHashingStrategy struct {
	config           consistent.Config
	consistentHasher *consistent.Consistent
}

func newConsistentHashingStrategy() Strategy {
	config := consistent.Config{
		PartitionCount:    1061,
		ReplicationFactor: 5,
		Load:              1.1,
		Hasher:            hasher{},
	}
	consistentHasher := consistent.New(nil, config)
	chStrategy := &consistentHashingStrategy{
		consistentHasher: consistentHasher,
		config:           config,
	}
	return chStrategy
}

func (*consistentHashingStrategy) GetName() string {
	return consistentHashingStrategyName
}

func (s *consistentHashingStrategy) GetCollectorForTarget(collectors map[string]*Collector, item *target.Item) (*Collector, error) {
	// This fork keys on the target's full post-relabel identity (item.Hash():
	// the relabeled label set plus job name, with __meta_* service-discovery
	// labels already excluded) instead of __address__ only, so endpoints
	// sharing a host:port but distinguished by any other label - a static
	// `shard` label, __param_*, or metrics path - spread across collectors.
	//
	// __scheme__/__metrics_path__/__param_* are only visible here when a
	// relabel_config writes them explicitly; ScrapeConfig-level metricsPath/
	// params/scheme fields are merged by Prometheus's own scrape manager and
	// never reach the allocator's labels at all, so this key can't spread
	// targets whose only differentiator is one of those config-level fields.
	//
	// Labels promoted from SD metadata via relabeling (e.g. netbox_rack) are
	// NOT excluded, unlike __meta_*-prefixed labels: if such a value can
	// change for the same address over time, this reintroduces the churn the
	// Jul-1 fix tried to avoid. Revisit with a job-scoped allow/deny-list if
	// that turns out to matter in practice.
	member := s.consistentHasher.LocateKey([]byte(item.Hash().String()))
	collectorName := member.String()
	collector, ok := collectors[collectorName]
	if !ok {
		return nil, fmt.Errorf("unknown collector %s", collectorName)
	}
	return collector, nil
}

func (s *consistentHashingStrategy) SetCollectors(collectors map[string]*Collector) {
	// we simply recreate the hasher with the new member set
	// this isn't any more expensive than doing a diff and then applying the change
	var members []consistent.Member

	if len(collectors) > 0 {
		members = make([]consistent.Member, 0, len(collectors))
		for _, collector := range collectors {
			members = append(members, collector)
		}
	}

	s.consistentHasher = consistent.New(members, s.config)
}

func (*consistentHashingStrategy) SetFallbackStrategy(Strategy) {}
