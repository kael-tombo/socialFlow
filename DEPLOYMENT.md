# Deploying SocialFlow.ai

SocialFlow.ai ships as a single static binary (Go backend with the web app
embedded) packaged as a minimal Alpine image. One container with SQLite and
local media storage is the supported reference deployment, and the
`deploy/socialflow/` bundle automates everything from source to a running,
health-gated stack.

This guide covers the standard Linux server path. For platform-specific
packaging (Coolify, Dokploy, Portainer, Dockge, CasaOS, ZimaOS) see the
templates in `deploy/`, and for the managed edge configuration see
`deploy/cloudflare/`. For a free hosting path on Oracle Cloud's Always Free
tier, see `docs/reference/installation/oracle-cloud-free.md`; for a free
ephemeral **test** deployment on Hugging Face Spaces, see
`deploy/hf-space/README.md`.

---

## 1. Prerequisites

| Requirement | Minimum | Notes |
|---|---|---|
| OS | Linux x86_64 or arm64 | Any distribution with Docker support |
| RAM | 2 GB | 4 GB recommended once AI/media features are enabled |
| Disk | 10 GB | Grows with media library and SQLite database |
| Docker | Engine 24+ with the buildx plugin | `docker buildx version` must succeed |
| Bun | 1.2+ | Only needed on the build machine (`curl -fsSL https://bun.sh/install \| bash`) |
| DNS | A/AAAA record to the server | Required for automatic HTTPS |

The build machine and the runtime host can be different: build with
`deploy/socialflow/deploy.sh --push` against a registry, then pull and run the
image on the server.

---

## 2. Quick start (one command)

```bash
git clone https://github.com/kael-tombo/socialFlow.git
cd socialFlow
cp deploy/socialflow/.env.prod.example deploy/socialflow/.env
$EDITOR deploy/socialflow/.env        # set URLs, domain, and both secrets
deploy/socialflow/deploy.sh
```

The script installs workspace dependencies, builds the frontend, packages it
into the server embed directory, builds the image via the
`frontend_artifact` build context, starts the stack, and gates completion on
`/api/v1/ready`. When it prints the success line, open
`http://localhost:8080`, register the first account, and then set
`OPENPOST_DISABLE_REGISTRATIONS=true` in `deploy/socialflow/.env` and restart
the stack to freeze registration.

Useful day-2 commands:

```bash
docker compose -f deploy/socialflow/docker-compose.prod.yml logs -f app   # follow logs
docker compose -f deploy/socialflow/docker-compose.prod.yml ps            # status
deploy/socialflow/deploy.sh --skip-build                                  # restart on existing image
```

---

## 3. HTTPS with the bundled reverse proxy

The stack ships an optional Caddy service that provisions and renews
Let's Encrypt certificates automatically.

1. Point the DNS record for your domain at the server and make sure ports 80
   and 443 are reachable from the internet.
2. In `deploy/socialflow/.env` set:

   ```bash
   SOCIALFLOW_DOMAIN=socialflow.example.com
   SOCIALFLOW_ACME_EMAIL=ops@example.com
   SOCIALFLOW_BIND=127.0.0.1     # app stays private; Caddy is the only public surface
   OPENPOST_APP_URL=https://socialflow.example.com
   OPENPOST_PUBLIC_URL=https://socialflow.example.com
   ```

3. Start with the proxy profile:

   ```bash
   docker compose -f deploy/socialflow/docker-compose.prod.yml --profile proxy up -d
   ```

While testing DNS or firewall changes, uncomment the staging `acme_ca` line in
`deploy/socialflow/Caddyfile` to avoid certificate rate limits, then comment it
back out for production issuance.

Existing reverse proxies work too: terminate TLS wherever you like and forward
to `127.0.0.1:8080`. Keep forwarding the `X-OpenPost-Proxy-*` header contract
described in `docs/reference/installation/reverse-proxy.md` if you want proxy
authentication instead of per-user login.

---

## 4. Configuration

The minimal production set lives in `deploy/socialflow/.env.prod.example`:

| Variable | Why it matters |
|---|---|
| `OPENPOST_APP_URL` | Canonical origin for links, OAuth callbacks, and emails. Set before first run. |
| `OPENPOST_PUBLIC_URL` | Root for API/asset URLs; defaults to `OPENPOST_APP_URL`. |
| `OPENPOST_JWT_SECRET` | Session signing key, 32+ characters (`openssl rand -base64 32`). |
| `OPENPOST_ENCRYPTION_KEY` | Encrypts connected-account credentials at rest. Rotating later requires the keyring procedure in `docs/reference/configuration/environment-variables.md`. |
| `OPENPOST_DISABLE_REGISTRATIONS` | Freeze sign-ups after the first admin exists. |

Everything else is optional and documented in the root `.env.example` — media
providers, AI providers, SMTP, OIDC, billing, and per-platform limits are all
environment-driven and can be added without rebuilding the image.

Data lives in the `socialflow_data` named volume mounted at `/data` (SQLite
database under `/data/db`, media under `/data/media`).

---

## 5. Backups and updates

**Backups.** Follow `docs/reference/operations/backups.md`. The short version:
stop writes (or use the documented SQLite backup flow), copy
`/data/db/openpost.db` together with `/data/media`, and test the restore path
before you need it. A minimal cron-friendly snapshot:

```bash
docker run --rm -v socialflow_socialflow_data:/data -v "$PWD/backups:/backup" \
  alpine tar czf "/backup/socialflow-$(date +%F).tar.gz" -C /data db media
```

**Updates.** Pull the latest revision and re-run the deploy script; the
database schema migrates automatically on boot:

```bash
git pull --ff-only
deploy/socialflow/deploy.sh
```

Images are content-addressed by version and commit labels, so rolling back is
`SOCIALFLOW_IMAGE=socialflow:<previous-tag> deploy/socialflow/deploy.sh --skip-build`.
Check `CHANGELOG.md` for breaking notes between versions.

---

## 6. Verification and smoke checks

After every deploy:

```bash
curl -fsS http://localhost:8080/api/v1/health   # process liveness
curl -fsS http://localhost:8080/api/v1/ready    # traffic gate — must pass
scripts/smoke-production-image.sh socialflow:latest   # full image smoke suite (build machine)
```

`/api/v1/ready` is the gate for load balancers and rollout automation;
`/api/v1/health` only proves the process is alive. The deploy script blocks
until readiness passes, so a green deploy line means the instance is genuinely
serving.

---

## 7. Registry publishing (CI-friendly)

To publish the image instead of only running it locally:

```bash
export SOCIALFLOW_REGISTRY=ghcr.io/kael-tombo
export PLATFORMS=linux/amd64,linux/arm64
deploy/socialflow/deploy.sh --push
```

Then point `SOCIALFLOW_IMAGE` in `deploy/socialflow/.env` at the pushed tag and
run `deploy/socialflow/deploy.sh --skip-build` on the server. The multi-arch
build requires a configured `docker buildx` builder with QEMU binfmt support
(`docker run --privileged --rm tonistiigi/binfmt --install all`).
