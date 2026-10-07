import type { NextConfig } from "next";

// Where torii's public listener (TORII_PUBLIC_ADDR) is reachable. Compose
// publishes it on 8190; the web image sets this to the torii service. Rewrites
// are resolved when the app is built.
const toriiUrl = process.env.TORII_URL ?? "http://localhost:8190";

// Everything the app loads comes from its own origin: the API and login are
// proxied through it, so connect-src needs no other host. Next writes inline
// scripts for hydration and inline styles, and a nonce would make every page
// dynamic, so script-src and style-src allow 'unsafe-inline' (and, for the dev
// server's refresh only, 'unsafe-eval'). The rest is as tight as it goes.
const isDev = process.env.NODE_ENV !== "production";
const contentSecurityPolicy = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${isDev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data:",
  "font-src 'self'",
  "connect-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

const securityHeaders = [
  { key: "Content-Security-Policy", value: contentSecurityPolicy },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
];

const nextConfig: NextConfig = {
  // A self-contained server for the Docker image.
  output: "standalone",
  // Next gzips what it proxies, and gzip holds back the small chunks of
  // torii's notification stream until the stream ends. Whatever fronts the app
  // in production compresses instead.
  compress: false,
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${toriiUrl}/:path*` },
      { source: "/auth/:path*", destination: `${toriiUrl}/auth/:path*` },
    ];
  },
};

export default nextConfig;
