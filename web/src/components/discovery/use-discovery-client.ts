import { createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMemo } from "react";

import { DiscoveryService } from "@/gen/shogun/api/v1/discovery_pb";

// useDiscoveryClient returns a client for DiscoveryService on the app transport.
export function useDiscoveryClient() {
  const transport = useTransport();
  return useMemo(() => createClient(DiscoveryService, transport), [transport]);
}
