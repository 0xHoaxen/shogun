# syntax=docker/dockerfile:1
# Build the web app image; the context is web/:
#   docker build -f deploy/docker/web.Dockerfile --build-arg TORII_URL=http://torii:8081 web

ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

FROM node:${NODE_VERSION}-alpine AS build
# Next resolves the /api and /auth rewrites when it builds, so torii's address
# has to be known here.
ARG TORII_URL
ENV TORII_URL=${TORII_URL} NEXT_TELEMETRY_DISABLED=1
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN test -n "${TORII_URL}" && npm run build

FROM node:${NODE_VERSION}-alpine
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
WORKDIR /app
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
USER node
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/login || exit 1
CMD ["node", "server.js"]
