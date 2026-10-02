# syntax=docker/dockerfile:1
# Build one service image from the repo root:
#   docker build --build-arg SERVICE=kagami -f deploy/docker/Dockerfile .

ARG GO_VERSION=1.25.4

FROM golang:${GO_VERSION} AS build
ARG SERVICE
ARG VERSION=dev
ARG COMMIT=none
ENV CGO_ENABLED=0 GOWORK=off GOTOOLCHAIN=local

WORKDIR /src
# The service module resolves pkg and gen/go through its replace directives.
COPY pkg ./pkg
COPY gen/go ./gen/go
COPY services/${SERVICE} ./services/${SERVICE}

WORKDIR /src/services/${SERVICE}
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    test -n "${SERVICE}" && \
    go build -trimpath \
      -ldflags "-s -w \
        -X github.com/0xHoaxen/shogun/pkg/version.Version=${VERSION} \
        -X github.com/0xHoaxen/shogun/pkg/version.Commit=${COMMIT}" \
      -o /out/service ./cmd/${SERVICE}

# distroless has no shell or curl, so there is no HEALTHCHECK here: compose and
# helm probe GET /healthz on HTTP_PORT (8080) and gRPC health on GRPC_PORT (9090).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/service /service
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/service"]
