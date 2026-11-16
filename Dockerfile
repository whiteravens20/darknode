# Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.
#
# The image that ships holds three things: one statically linked binary, the
# world's data files, and the built web client. There is no shell, no package
# manager and no interpreter in it, so a flaw in the game gives an attacker
# nothing to run.
#
# Base images are pinned by exact version and digest. Dependabot moves both.

# ── The web client ───────────────────────────────────────────────────────────
FROM node:24.22.2-alpine@sha256:a34ab6d7f6f4813f4d0c5469d74685604b962fbdbf5793a7bc7b4998ecb867c8 AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
# Install exactly what the lockfile pins, and run none of the packages' own
# install scripts.
RUN npm ci --ignore-scripts

COPY web/ ./
RUN npm run build

# ── The server ───────────────────────────────────────────────────────────────
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build

WORKDIR /src
# The module files first, so the dependency download is cached while the code
# changes.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG VERSION=dev
# CGO off makes the binary static, so it runs on an image with no libc.
# -trimpath keeps build paths out of it; the ldflags drop the symbol table.
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/darknode ./cmd/darknode

# ── What ships ───────────────────────────────────────────────────────────────
FROM scratch AS final

# The certificate roots, for a database connection that uses TLS.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=build /out/darknode /darknode
COPY data/world /data/world
COPY --from=web /src/web/dist /web
COPY LICENSE NOTICE /

# A user that owns nothing. The game writes only to the database, so the
# filesystem can be read-only.
USER 65532:65532

ENV DARKNODE_LISTEN=":8080" \
    DARKNODE_WORLD_DIR="/data/world" \
    DARKNODE_WEB_DIR="/web"

EXPOSE 8080

# The binary answers its own health check, because the image has no shell and
# no curl to do it with.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/darknode", "healthcheck"]

ENTRYPOINT ["/darknode"]
CMD ["serve"]
