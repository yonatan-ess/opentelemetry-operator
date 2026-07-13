// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package allocation

import (
	"fmt"
	"strings"

	"github.com/buraksezer/consistent"
	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/open-telemetry/opentelemetry-operator/cmd/otel-allocator/internal/target"
)

const consistentHashingStrategyName = "consistent-hashing"

// endpointKeySeparator delimits the components of the scrape-URL hash key. It
// matches the separator Prometheus uses for label hashing and won't appear in
// label names.
const endpointKeySeparator = '\xff'

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
	// This fork keys on the target's scrape URL (address, scheme, metrics path,
	// and query params) instead of __address__ only, so endpoints sharing a
	// host:port but differing by path or params spread across collectors. Only
	// the URL labels are hashed, so changes to other, mutable labels (instance
	// metadata, service-discovery annotations, etc.) do not move a target.
	member := s.consistentHasher.LocateKey([]byte(endpointHashKey(item)))
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

// endpointHashKey builds a stable key from the parts that make up a target's
// scrape URL: its address (item.TargetURL, the same value the address-only
// strategy hashes), scheme, metrics path, and query params. Labels are read in
// their (sorted) order, so the key is deterministic for a given endpoint.
func endpointHashKey(item *target.Item) string {
	ls := item.Labels
	var sb strings.Builder
	sb.WriteString(item.TargetURL)
	sb.WriteByte(endpointKeySeparator)
	sb.WriteString(ls.Get(model.SchemeLabel))
	sb.WriteByte(endpointKeySeparator)
	sb.WriteString(ls.Get(model.MetricsPathLabel))
	ls.Range(func(l labels.Label) {
		if strings.HasPrefix(l.Name, model.ParamLabelPrefix) {
			sb.WriteByte(endpointKeySeparator)
			sb.WriteString(l.Name)
			sb.WriteByte('=')
			sb.WriteString(l.Value)
		}
	})
	return sb.String()
}
