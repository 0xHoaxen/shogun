import { createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMemo } from "react";

import { LearningService } from "@/gen/shogun/api/v1/learning_pb";

// useLearningClient returns a client for LearningService on the app transport.
export function useLearningClient() {
  const transport = useTransport();
  return useMemo(() => createClient(LearningService, transport), [transport]);
}
