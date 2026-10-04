import type { NextConfig } from "next";

// Where torii's public listener (TORII_PUBLIC_ADDR) is reachable. Compose
// publishes it on 8090; in the stack the web container sets this to the torii
// service. Rewrites are resolved when the app is built.
const toriiUrl = process.env.TORII_URL ?? "http://localhost:8090";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${toriiUrl}/:path*` },
      { source: "/auth/:path*", destination: `${toriiUrl}/auth/:path*` },
    ];
  },
};

export default nextConfig;
