import { createConnectTransport } from "@connectrpc/connect-web";

// The browser talks to torii only through the web app's own origin: Next
// proxies /api and /auth to torii (see next.config.ts), so the session cookie
// stays first-party and no CORS is needed.
export const API_BASE_URL = "/api";

export const transport = createConnectTransport({
  baseUrl: API_BASE_URL,
});
