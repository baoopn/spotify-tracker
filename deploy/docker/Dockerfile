# syntax=docker/dockerfile:1.7
#
# One image, all binaries: api (incl. embedded admin SPA + public page), poller, enricher,
# migrate, importer. The service to run is chosen by the container command,
# e.g. `command: ["/usr/local/bin/poller"]`.

ARG GO_VERSION=1.27
ARG NODE_VERSION=22

# ─── Frontends (admin SPA + public page) ──────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web-base
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH CI=true
RUN corepack enable

FROM web-base AS web-admin
WORKDIR /web
COPY web/admin/package.json web/admin/pnpm-lock.yaml web/admin/pnpm-workspace.yaml* ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
COPY web/admin/ ./
RUN pnpm build

FROM web-base AS web-public
WORKDIR /web
COPY web/public/package.json web/public/pnpm-lock.yaml web/public/pnpm-workspace.yaml* ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
COPY web/public/ ./
RUN pnpm build

# ─── Go build ─────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
COPY --from=web-admin /web/build ./web/admin/build
COPY --from=web-public /web/build ./web/public/build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w -X github.com/baoopn/spotify-tracker/internal/platform/app.Version=${VERSION}" \
      -o /out/ ./cmd/... \
 && mkdir -p /tz && cp "$(go env GOROOT)/lib/time/zoneinfo.zip" /tz/zoneinfo.zip

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
LABEL org.opencontainers.image.title="spotify-tracker" \
      org.opencontainers.image.description="Spotify listening tracker: API + admin dashboard, poller and enricher" \
      org.opencontainers.image.source="https://github.com/baoopn/spotify-tracker" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/ /usr/local/bin/

# Use the Go toolchain's IANA tz database instead of the base image's
# /usr/share/zoneinfo, which lags real-world rule changes (e.g. Alberta's
# permanent UTC-6). Go reads ZONEINFO before the system database.
COPY --from=build /tz/zoneinfo.zip /usr/local/share/zoneinfo.zip
ENV ZONEINFO=/usr/local/share/zoneinfo.zip

USER nonroot:nonroot
EXPOSE 8080 9000 9090

# Each binary supports `healthcheck`, which probes its own ops server.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/usr/local/bin/api", "healthcheck"]

ENTRYPOINT []
CMD ["/usr/local/bin/api"]
