import { createConnectQueryKey } from "@connectrpc/connect-query";

import { DiscoveryService } from "@/gen/shogun/api/v1/discovery_pb";

// postingsKey matches every cached ListDiscoveryPostings result, whatever the
// filter, so a change refetches the postings.
export const postingsKey = createConnectQueryKey({
  schema: DiscoveryService.method.listDiscoveryPostings,
  cardinality: "infinite",
});

// sourcesKey matches the cached ListDiscoverySources result.
export const sourcesKey = createConnectQueryKey({
  schema: DiscoveryService.method.listDiscoverySources,
  cardinality: "finite",
});

// preferencesKey matches the cached GetDiscoveryPreferences result.
export const preferencesKey = createConnectQueryKey({
  schema: DiscoveryService.method.getDiscoveryPreferences,
  cardinality: "finite",
});
