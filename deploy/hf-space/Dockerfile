# =============================================================================
# SocialFlow.ai — Hugging Face Spaces (Docker) test deployment
# =============================================================================
# Self-contained image build for HF Spaces, where only a plain
# `docker build` runs: this file performs the full pipeline that
# deploy/socialflow/deploy.sh otherwise orchestrates on a build machine:
#
#   1. bun install                      (workspace dependencies)
#   2. apps/web production build        (SvelteKit -> apps/web/build)
#   3. scripts/package-frontend.mjs     (embed -> apps/server/cmd/openpost/public)
#   4. Go build with embedded frontend  (CGO + SQLite, same as upstream CI)
#
# The runtime stage mirrors deploy/docker/Dockerfile byte-for-byte where it
# matters (base pins, packages, user, env, healthcheck, entrypoint).
#
# Build time on free Spaces hardware (2 vCPU): roughly 10–20 minutes.
# =============================================================================

# --- Stage 1: frontend --------------------------------------------------------
# Vite 8 requires Node 20.19+/22.12+; bun 1.3.11 matches packageManager.
# Bun's npm package ships glibc binaries, so the Debian-based Node image works.
FROM docker.io/library/node:22-bookworm-slim AS frontend-builder

RUN npm install --global bun@1.3.11

WORKDIR /repo

COPY . .

RUN bun install --frozen-lockfile
RUN cd apps/web && bun run build
RUN bun scripts/package-frontend.mjs

# --- Stage 2: backend (same builder pin as deploy/docker/Dockerfile) ----------
FROM docker.io/library/golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS backend-builder

ARG VERSION=dev
ARG COMMIT=unknown

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY apps/server/go.mod apps/server/go.sum ./apps/server/
RUN --mount=type=cache,target=/go/pkg/mod \
    cd apps/server && go mod download

COPY apps/server/ ./apps/server/
# Package the frontend-owned build output at the backend embed boundary.
COPY --from=frontend-builder /repo/apps/server/cmd/openpost/public ./apps/server/cmd/openpost/public

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd apps/server && CGO_ENABLED=1 GOOS=linux go build -buildvcs=false -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o openpost ./cmd/openpost

# --- Stage 3: runtime (mirrors deploy/docker/Dockerfile) ----------------------
FROM docker.io/library/alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS runtime

ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="SocialFlow.ai" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/kael-tombo/socialFlow" \
      org.opencontainers.image.base.name="docker.io/library/alpine:3.24.2"

RUN apk add --no-cache ca-certificates ffmpeg tzdata sqlite \
    'libcrypto3>=3.5.8-r0' 'libssl3>=3.5.8-r0'

RUN adduser -D -u 1000 openpost

WORKDIR /app

COPY --from=backend-builder /app/apps/server/openpost .

RUN mkdir -p /data/media /data/db && chown -R openpost:openpost /data

USER openpost

ENV OPENPOST_PORT=8080
ENV OPENPOST_EDITION=selfhost
ENV OPENPOST_DATABASE_DRIVER=sqlite
ENV OPENPOST_DATABASE_PATH=/data/db/openpost.db
ENV OPENPOST_STORAGE_DRIVER=local
ENV OPENPOST_MEDIA_PATH=/data/media
ENV OPENPOST_DIAGNOSTICS_STATE_FILE=/data/diagnostics-installation-id

EXPOSE 8080

# Container health is process liveness. Traffic and rollout gates use /ready.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/v1/health || exit 1

STOPSIGNAL SIGTERM

CMD ["./openpost", "all"]
