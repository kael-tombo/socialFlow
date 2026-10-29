---
title: SocialFlow.ai
emoji: 🚀
colorFrom: indigo
colorTo: purple
sdk: docker
app_port: 8080
pinned: false
short_description: SocialFlow.ai — self-hosted social media management (test)
---

# SocialFlow.ai on Hugging Face Spaces

This directory contains everything needed to run the **full SocialFlow.ai
stack** (Go server + embedded web app + SQLite) as a free Hugging Face Spaces
Docker deployment for **testing purposes**. It exists because Spaces only run
a plain `docker build` — this bundle performs the complete build pipeline
(frontend, embed, Go compile) inside a single self-contained Dockerfile, so
no build machine or registry is required.

Free-Space characteristics to be aware of before you start:

| Aspect | Behavior on the free tier |
|---|---|
| Cost | Free (2 vCPU, 16 GB RAM — comfortably above the app's 2 GB minimum) |
| URL | Public: `https://<owner>-<space-name>.hf.space` |
| First build | 10–20 minutes (bun install + Vite build + Go/CGO compile) |
| Data | **Ephemeral** — SQLite and media reset on every restart/rebuild |
| Idle sleep | Space sleeps after ~48 h without visits; waking rebuilds state but data was already reset at stop |
| Secrets | Set in Space settings (Settings → Variables and secrets); injected at runtime only |

This makes it ideal for trying the product end-to-end, and unsuitable for
keeping real data — graduate to the Docker Compose path in `DEPLOYMENT.md`
(or the Oracle Cloud guide in
`docs/reference/installation/oracle-cloud-free.md`) when you want
persistence.

---

## 1. One-time setup

### 1.1 Create the Space

1. On huggingface.co, click **New → Space**.
2. Name it (for example `socialflow-test`), SDK: **Docker**, license as you
   prefer, visibility **Public** (private Spaces require paid hardware).
3. Leave the Space empty — the sync below fills it.

### 1.2 Configure the GitHub side

The repo ships `.github/workflows/sync-hf-space.yml`, which snapshots the
repository (with a Space-specific `Dockerfile` and `README.md`) and pushes
it to your Space. It needs two repository settings on GitHub:

| Where | Name | Value |
|---|---|---|
| Settings → Secrets and variables → Actions → **Secrets** | `HF_TOKEN` | A Hugging Face **write** token (hf.co/settings/tokens) |
| Settings → Secrets and variables → Actions → **Variables** | `HF_SPACE` | `<owner>/<space-name>`, e.g. `kael-tombo/socialflow-test` |

### 1.3 Sync and build

Run **Actions → Sync SocialFlow.ai to Hugging Face Space → Run workflow**.
The first push uploads the repository snapshot (a few hundred megabytes —
expect several minutes), then the Space builds for 10–20 minutes. Watch
progress on the Space page. When the build finishes, the app is live at
`https://<owner>-<space-name>.hf.space`.

### 1.4 Alternative: manual push (no GitHub Actions)

```bash
git clone https://github.com/kael-tombo/socialFlow.git && cd socialFlow
git lfs install
git lfs track "apps/web/static/moss-tts/vendor/ort/*.wasm"
git add --renormalize .
cp deploy/hf-space/Dockerfile Dockerfile
cp deploy/hf-space/README.md README.md
git checkout --orphan space-sync && git add -A
git commit -m "SocialFlow.ai Spaces snapshot"
git remote add space "https://<HF_USERNAME>:<HF_TOKEN_WRITE>@huggingface.co/spaces/<owner>/<space-name>"
git push space space-sync:main --force
```

> The `git lfs track` line matters: one bundled file (the ~13 MB
> `ort-wasm-simd-threaded.wasm` TTS runtime) exceeds Hugging Face's 10 MB
> non-LFS limit. The workflow handles this automatically.

---

## 2. Required Space secrets

Before (or right after) the first build, add these in
**Space Settings → Variables and secrets** as *secrets*, not plain
variables. Generate each value with `openssl rand -base64 32`.

| Secret | Value |
|---|---|
| `OPENPOST_APP_URL` | `https://<owner>-<space-name>.hf.space` — canonical origin for links and OAuth callbacks; must match the browser URL exactly |
| `OPENPOST_PUBLIC_URL` | Same value (or omit; it defaults to `OPENPOST_APP_URL`) |
| `OPENPOST_JWT_SECRET` | Random 32+ character string — signs sessions |
| `OPENPOST_ENCRYPTION_KEY` | Random 32+ character string — encrypts connected-account credentials at rest |
| `OPENPOST_DISABLE_REGISTRATIONS` | Set to `true` **after** you register the first (admin) account; changing secrets triggers a restart |

Everything else (SMTP, AI providers, per-platform credentials) is optional
and documented in the root `.env.example`; add them as additional secrets as
needed. Changing a secret or variable restarts the Space automatically.

---

## 3. First run

1. Wait for the Space to report **Running**, then open the public URL.
2. Register the first account — it becomes the administrator.
3. Set `OPENPOST_DISABLE_REGISTRATIONS=true` in the secrets (the Space
   restarts; ephemeral data survives a settings-restart, only rebuilds and
   sleeps reset it).
4. Smoke-check: `curl -fsS https://<owner>-<space-name>.hf.space/api/v1/ready`
   should return a success response — this is the same readiness gate the
   Docker Compose deployment uses.

---

## 4. Updating the test deployment

Re-run the sync workflow (or repeat the manual push). The Space rebuilds the
image and restarts; the database schema migrates automatically on boot, but
remember data itself does not survive rebuilds on the free tier.

If the build fails, check the Space's **Logs → Build** tab. The most common
causes are an interrupted first push (retry the sync) or a transient
registry hiccup during `bun install` (Factory-rebuild from Space settings).
