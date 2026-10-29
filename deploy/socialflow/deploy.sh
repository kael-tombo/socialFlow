#!/usr/bin/env bash
# =============================================================================
# SocialFlow.ai — one-command production deployment
# =============================================================================
# Builds the production image from this repository and brings the stack up.
#
# Pipeline (mirrors upstream release CI):
#   1. bun install                                  (workspace dependencies)
#   2. apps/web production build                    (SvelteKit -> apps/web/build)
#   3. scripts/package-frontend.mjs                 (embed -> apps/server/cmd/openpost/public)
#   4. docker buildx build                          (frontend_artifact context -> image)
#   5. docker compose up -d + readiness gate        (waits for /api/v1/ready)
#
# Usage:
#   deploy/socialflow/deploy.sh                    # build + start
#   deploy/socialflow/deploy.sh --skip-build       # reuse existing image
#   deploy/socialflow/deploy.sh --push             # build, tag, push to registry
#
# Environment overrides:
#   SOCIALFLOW_IMAGE   image name:tag          (default socialflow:latest)
#   SOCIALFLOW_REGISTRY registry prefix        (e.g. ghcr.io/kael-tombo)
#   PLATFORMS          buildx platforms        (default host platform)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
EMBED_DIR="${REPO_ROOT}/apps/server/cmd/openpost/public"
COMPOSE="docker compose -f ${SCRIPT_DIR}/docker-compose.prod.yml"
SKIP_BUILD=0
PUSH=0

for arg in "$@"; do
  case "${arg}" in
    --skip-build) SKIP_BUILD=1 ;;
    --push) PUSH=1 ;;
    -h|--help) sed -n '2,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: ${arg} (see --help)" >&2; exit 64 ;;
  esac
done

log()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# -----------------------------------------------------------------------------
# 0. Prerequisites + environment resolution
# -----------------------------------------------------------------------------
command -v bun >/dev/null 2>&1 || fail "bun is required: https://bun.sh"
command -v docker >/dev/null 2>&1 || fail "docker is required: https://docs.docker.com/engine"
docker buildx version >/dev/null 2>&1 || fail "docker buildx plugin is required"
docker info >/dev/null 2>&1 || fail "docker daemon is not reachable"

ENV_FILE="${SCRIPT_DIR}/.env"
if [[ ! -f "${ENV_FILE}" ]]; then
  if [[ -f "${REPO_ROOT}/.env" ]]; then
    cp "${REPO_ROOT}/.env" "${ENV_FILE}"
    log "copied repository-root .env to deploy/socialflow/.env — review it before going public"
  else
    fail "missing deploy/socialflow/.env — copy .env.prod.example and edit it first"
  fi
fi

# Pull SOCIALFLOW_* settings from .env without sourcing arbitrary shell code.
env_value() {
  local key="$1"
  local value
  value="$(grep -E "^${key}=" "${ENV_FILE}" 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '\"'"'"'' || true)"
  printf '%s' "${value}"
}
SOCIALFLOW_PORT="${SOCIALFLOW_PORT:-$(env_value SOCIALFLOW_PORT)}"
SOCIALFLOW_PORT="${SOCIALFLOW_PORT:-8080}"
SOCIALFLOW_BIND="${SOCIALFLOW_BIND:-$(env_value SOCIALFLOW_BIND)}"
SOCIALFLOW_IMAGE="${SOCIALFLOW_IMAGE:-$(env_value SOCIALFLOW_IMAGE)}"
IMAGE="${SOCIALFLOW_IMAGE:-${SOCIALFLOW_REGISTRY:+${SOCIALFLOW_REGISTRY}/}socialflow:latest}"

cd "${REPO_ROOT}"

if [[ ${SKIP_BUILD} -eq 1 ]]; then
  log "skipping build (--skip-build), using image: ${IMAGE}"
else
  # ---------------------------------------------------------------------------
  # 1. Workspace dependencies
  # ---------------------------------------------------------------------------
  log "installing workspace dependencies (bun install)"
  bun install --frozen-lockfile

  # ---------------------------------------------------------------------------
  # 2. Frontend production build
  # ---------------------------------------------------------------------------
  log "building frontend (apps/web -> apps/web/build)"
  (cd apps/web && bun run build)

  # ---------------------------------------------------------------------------
  # 3. Package frontend into the server embed directory
  # ---------------------------------------------------------------------------
  log "packaging frontend into server embed directory"
  rm -rf "${EMBED_DIR}"
  bun scripts/package-frontend.mjs
  [[ -f "${EMBED_DIR}/index.html" ]] || fail "embed packaging did not produce index.html"

  # ---------------------------------------------------------------------------
  # 4. Docker image build (frontend_artifact named context)
  # ---------------------------------------------------------------------------
  VERSION="$(git -C "${REPO_ROOT}" describe --tags --always 2>/dev/null || echo dev)"
  COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  BUILD_ARGS=(--build-arg "VERSION=${VERSION}" --build-arg "COMMIT=${COMMIT}")
  [[ -n "${PLATFORMS:-}" ]] && BUILD_ARGS+=(--platform "${PLATFORMS}")

  log "building image ${IMAGE}"
  docker buildx build \
    --load \
    --build-context "frontend_artifact=${EMBED_DIR}" \
    --file deploy/docker/Dockerfile \
    --tag "${IMAGE}" \
    "${BUILD_ARGS[@]}" \
    "${REPO_ROOT}"

  # ---------------------------------------------------------------------------
  # 5a. Optional registry push
  # ---------------------------------------------------------------------------
  if [[ ${PUSH} -eq 1 ]]; then
    [[ "${IMAGE}" == */* ]] || fail "--push requires SOCIALFLOW_IMAGE or SOCIALFLOW_REGISTRY with a registry path"
    log "pushing ${IMAGE}"
    docker push "${IMAGE}"
  fi
fi

# -----------------------------------------------------------------------------
# 5b. Start the stack
# -----------------------------------------------------------------------------
log "starting stack (compose project: socialflow)"
${COMPOSE} up -d --remove-orphans

# -----------------------------------------------------------------------------
# 6. Readiness gate
# -----------------------------------------------------------------------------
log "waiting for /api/v1/ready"
READY=0
for _ in $(seq 1 60); do
  if curl --silent --fail "http://localhost:${SOCIALFLOW_PORT:-8080}/api/v1/ready" >/dev/null 2>&1; then
    READY=1
    break
  fi
  sleep 2
done

if [[ ${READY} -ne 1 ]]; then
  echo "warning: readiness gate timed out after 120s — inspect:" >&2
  echo "  ${COMPOSE} logs app" >&2
  exit 1
fi

log "SocialFlow.ai is live: http://localhost:${SOCIALFLOW_PORT:-8080}"
log "register the first admin account, then set OPENPOST_DISABLE_REGISTRATIONS=true"
${COMPOSE} ps
