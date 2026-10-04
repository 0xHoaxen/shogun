import type { NextConfig } from "next";

// Where torii's public listener (TORII_PUBLIC_ADDR) is reachable. Compose
// publishes it on 8190; the web image sets this to the torii service. Rewrites
// are resolved when the app is built.
const toriiUrl = process.env.TORII_URL ?? "http://localhost:8190";

const nextConfig: NextConfig = {
  // A self-contained server for the Docker image.
  output: "standalone",
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${toriiUrl}/:path*` },
      { source: "/auth/:path*", destination: `${toriiUrl}/auth/:path*` },
    ];
  },
};

export default nextConfig;
