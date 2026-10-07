import { createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMemo } from "react";

import { ProfileService } from "@/gen/shogun/api/v1/profile_pb";

// useProfileClient returns a client for ProfileService on the app transport.
export function useProfileClient() {
  const transport = useTransport();
  return useMemo(() => createClient(ProfileService, transport), [transport]);
}
