import { Code, ConnectError, type Interceptor, type Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

// The browser talks to torii only through the web app's own origin: Next
// proxies /api and /auth to torii (see next.config.ts), so the session cookie
// stays first-party and no CORS is needed.
export const API_BASE_URL = "/api";

export const LOGIN_PATH = "/login";

function notifyWhenUnauthenticated(onUnauthenticated: () => void): Interceptor {
  return (next) => async (req) => {
    try {
      return await next(req);
    } catch (error) {
      if (ConnectError.from(error).code === Code.Unauthenticated) {
        onUnauthenticated();
      }
      throw error;
    }
  };
}

// createTransport returns the Connect transport. A missing or expired session
// on any call runs onUnauthenticated, which sends the browser to the login page.
export function createTransport(onUnauthenticated: () => void): Transport {
  return createConnectTransport({
    baseUrl: API_BASE_URL,
    interceptors: [notifyWhenUnauthenticated(onUnauthenticated)],
  });
}
