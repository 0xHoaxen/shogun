package bus

import (
	"slices"
	"strings"

	"github.com/0xHoaxen/shogun/pkg/config"
)

const addrEnvSuffix = "_ADDR"

// AllConsumers returns every consumer service named in the routes, sorted and
// without duplicates.
func AllConsumers() []string {
	return consumersOf(routes)
}

func consumersOf(table map[string][]string) []string {
	seen := map[string]struct{}{}
	for _, consumers := range table {
		for _, c := range consumers {
			seen[c] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Targets maps each consumer to the address in its <NAME>_ADDR variable (for
// example TAIKO_ADDR). A consumer with no address is left out rather than
// failing startup, because a service only needs the consumers of the events
// it produces; GRPCBus.Deliver reports a missing endpoint by name if a routed
// event ever needs one.
func Targets(lookup config.LookupFunc, consumers []string) map[string]string {
	targets := make(map[string]string, len(consumers))
	for _, c := range consumers {
		if addr, ok := lookup(strings.ToUpper(c) + addrEnvSuffix); ok && addr != "" {
			targets[c] = addr
		}
	}
	return targets
}
