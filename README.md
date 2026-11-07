# SocialFlow Lite

The smallest working SocialFlow.ai: one Go binary and one SQLite file that connect **LinkedIn (personal profile)**
and **X** accounts and publish text posts and X threads to them, now or at a scheduled time.

It implements exactly the API Banyan needs — `banyan-socialflow-contract/0.1`, operations C1–C10 in Banyan's
`src/lib/distribution/socialflow/contract.ts` — so Banyan's existing SocialFlow adapter works against it unchanged.

Derived from [kael-tombo/socialFlow](https://github.com/kael-tombo/socialFlow), which is a rebrand of
[OpenPost](https://github.com/getopenpost/openpost). The LinkedIn and X request shapes (endpoints, headers,
OAuth flows, commentary escaping, API version lag) follow OpenPost's adapters in `apps/server/internal/platform`.
Licensed **AGPL-3.0** like its upstream — see [LICENSE](LICENSE). If you run a modified version as a network
service, you must offer its source to its users.

Lives on the `socialflow-lite` branch of [kael-tombo/socialFlow](https://github.com/kael-tombo/socialFlow/tree/socialflow-lite), with its own history (it shares no files with `main`):

```bash
git clone -b socialflow-lite --single-branch https://github.com/kael-tombo/socialFlow.git socialflow-lite
```

## What it does and does not do

| Does | Does not |
| --- | --- |
| Connect LinkedIn personal profiles (`openid profile w_member_social`) | LinkedIn company pages (need Community Management API review) |
| Connect X accounts (OAuth 1.0a user context) | Instagram, TikTok, YouTube, Facebook, Threads, … |
| Publish text posts; X threads (Banyan's `1/ … 2/ …` format) | Images, video, documents, polls |
| Schedule posts (`scheduled_at`), published by an in-process scheduler | Analytics — `GET /posts/:id/analytics` honestly answers `not_found` |
| Idempotency: same `Idempotency-Key` never publishes twice from a known state; a failed thread resumes where it stopped; an unknown outcome stops for a person | LinkedIn token refresh (member tokens last ~60 days; the account then shows `error` and must be reconnected; a post scheduled past the expiry is refused when it is created) |
| Encrypt tokens at rest (AES-256-GCM); store only API-key hashes | Webhooks, inbox, comments, teams, billing, UI |

Upstream is ~7,800 files across a Go server, a web app, mobile, CLI and docs. This is ~2,000 lines of Go (plus ~600 of tests).

## API

All `/v1` routes need `Authorization: Bearer <api key>`. Errors are always
`{"error":{"code":"…","message":"…"}}` with code one of `auth_failed`, `revoked`, `rate_limited`,
`unsupported_format`, `validation`, `not_found`, `provider_unavailable`.

| Contract | Route | Notes |
| --- | --- | --- |
| C1 | `GET /v1/me` | `{organization_id, name}` |
| C2 | `POST /v1/accounts/connect` `{platform}` | `{connect_url, expires_at}` — send the founder's browser there; single use, 30 min |
| C3 | `GET /v1/accounts` | `{accounts:[{id, platform, handle, status, capabilities, token_expires_at}]}`; status `active` \| `error` (reconnect; also shown as soon as a token expires) \| `revoked`. `token_expires_at` is null for X |
| C4 | `GET /v1/accounts/:id/capabilities` | `{publish, schedule, analytics, formats, max_length}` |
| C5/C6 | `POST /v1/posts` `{channel, text, account_id?, title?, scheduled_at?}` + **required** `Idempotency-Key` | See below |
| C7 | `GET /v1/posts/:id` | Post with `status` `queued` \| `scheduled` \| `published` \| `failed`, `url`, `error`, `external_ids` |
| C8 | `DELETE /v1/posts/:id` | Cancels a scheduled (or failed) post; idempotent. 409 if it is being published or already published |
| C9 | `GET /v1/posts/:id/analytics` | Always `404 not_found` (not collected) |
| C10 | `DELETE /v1/accounts/:id` | Destroys the token locally, asks the provider to revoke it (`provider_revoked`) |

**Publishing semantics** (what Banyan's adapter relies on):

- `201` + `status: "published"` — the provider confirmed the post; `url` is its permalink.
- `201` + `status: "scheduled"` — will be published at `scheduled_at` (anything under 30 s away publishes now).
- `201` + `status: "failed"` — a permanent failure (`auth_failed`, `validation`, …). Fix the cause, then retry.
- `429 rate_limited` (with `Retry-After`) or `502 provider_unavailable` — transient. **Retry with the same
  `Idempotency-Key`**: the post resumes (a thread continues from the next post).
- `200` — the key was seen before; the stored post is returned, even if the account has since been disconnected.
  A reused key with a different body is `422`.
- Without `account_id`, the single active account for `channel` is used.
- A scheduled post retries transient failures by itself with backoff, up to 5 attempts, then ends `failed`.

**When nobody knows whether a post went out.** If the connection drops after the request was sent, a gateway times
out, a provider accepts a post but returns no id, or the process dies mid-publish, the post ends `failed` with code
`validation` and a message saying it may have gone out. A replay with the same key returns that post and **does not
publish again**, because a duplicate in someone's name is worse than a failed post a person can check. To send it
again, use a new key. This is the one case where a thread cannot resume by itself. A provider that gives no
idempotency key of its own leaves no way to do better.

## MCP

The same operations are also served as **Model Context Protocol tools** (nine) at `POST /mcp` (Streamable HTTP, stateless,
JSON responses; protocol versions `2025-06-18` and `2025-03-26`). Authenticate with the same `Authorization: Bearer <api key>`.

| Tool | Mirrors | Annotations |
| --- | --- | --- |
| `whoami` | C1 `GET /v1/me` | read-only |
| `list_accounts` | C3 | read-only |
| `get_account_capabilities` | C4 | read-only |
| `start_account_connection` | C2, returns a `connect_url` that only a person can complete | not destructive |
| `publish_post` | C5, requires `idempotency_key` | idempotent, open world |
| `schedule_post` | C6 | idempotent, open world |
| `get_post_status` | C7 | read-only |
| `cancel_post` | C8 | destructive, idempotent |
| `disconnect_account` | C10 | **destructive** |

Each tool runs through the REST handler it mirrors, so validation, idempotency (one store for both transports),
limits and error codes are identical. Every result carries `structuredContent: { status, body, retry_after? }`:
the REST status and body. `isError` is set from status 400 and up, so a client can apply the contract's retry rules
unchanged. Checked with the official `@modelcontextprotocol/sdk` client (1.17.4): handshake, tool listing, publish,
idempotent replay and tool errors.

Example client config (any MCP client that supports Streamable HTTP):

```json
{ "mcpServers": { "socialflow": { "url": "https://social.example.com/mcp", "headers": { "Authorization": "Bearer sfl_…" } } } }
```

The server instructions tell agents to publish only human-approved text and always pass an idempotency key. Accounts
can only be connected by a person through the `connect_url`.

**Operate it in plain language with OpenCode.** [`opencode/`](opencode/) holds an OpenCode agent wired to these tools:
ask which accounts are connected or what happened to a post; publishing, scheduling and cancelling ask you every
time, and disconnecting is denied. See [opencode/README.md](opencode/README.md).

## Set up the developer consoles

Set `PUBLIC_URL` and `ENCRYPTION_KEY`, then run:

```bash
./socialflow-lite doctor --offline
```

It prints exactly what to enter in each console: the LinkedIn products, redirect URL and scopes, and the X OAuth
1.0a setting, permissions and callback URL. Paste the keys you get back into `.env`, then run `doctor` again
**without** `--offline`. It now checks them live, with no user sign-in needed:

- **X:** it requests a real OAuth 1.0a request token. That proves the API key and secret, that OAuth 1.0a is
  enabled, and that the callback URL is registered exactly as this service sends it.
- **LinkedIn:** it exchanges a deliberately invalid code. LinkedIn answers `invalid_client` only when the client id
  or secret is wrong, so any other refusal proves them. The redirect URL is confirmed the first time you connect
  an account.

`doctor` exits non-zero on any FAIL line.

## Run it

```bash
cp .env.example .env              # fill in, see below
go build -o socialflow-lite ./cmd/socialflow-lite
./socialflow-lite doctor                  # check the configuration
./socialflow-lite keys create "Banyan"    # prints the API key once
./socialflow-lite serve
```

Keys: `keys list` shows key ids (never secrets). To rotate, run `keys add <org_id>` for a second key, switch the
client to it, then `keys revoke <key_id>` the old one.

Or with Docker: `docker build -t socialflow-lite . && docker run --env-file .env -p 8080:8080 -v sfl-data:/data socialflow-lite`
(then `docker run --rm --env-file .env -v sfl-data:/data socialflow-lite keys create "Banyan"`).

| Variable | Required | Meaning |
| --- | --- | --- |
| `PUBLIC_URL` | yes | Public address browsers reach, `https://` outside localhost. Callbacks are `PUBLIC_URL/oauth/linkedin/callback` and `PUBLIC_URL/oauth/x/callback` |
| `ENCRYPTION_KEY` | yes | 64 hex chars: `openssl rand -hex 32`. Losing it means reconnecting every account |
| `DATABASE_PATH` | no | Default `data/socialflow-lite.db` (`/data/socialflow-lite.db` in Docker) |
| `PORT` | no | Default `8080` |
| `LINKEDIN_CLIENT_ID`, `LINKEDIN_CLIENT_SECRET` | for LinkedIn | From a LinkedIn developer app with the **Share on LinkedIn** and **Sign In with LinkedIn using OpenID Connect** products; add the callback URL under Auth |
| `LINKEDIN_API_VERSION` | no | `YYYYMM`; defaults to last month (LinkedIn versions are not always live on the 1st) |
| `X_CONSUMER_KEY`, `X_CONSUMER_SECRET` | for X | API key/secret of an X app with **Read and write** permission, OAuth 1.0a enabled, callback URL registered. X API is pay-per-use |

`LINKEDIN_AUTH_BASE`, `LINKEDIN_API_BASE` and `X_API_BASE` override the provider hosts, for an egress proxy or local
end-to-end tests against stand-in servers. Leave them unset in production.

A provider whose credentials are missing is reported as `provider_unavailable` on connect — never faked.

## Connect Banyan to it

1. Deploy SocialFlow Lite on a **public HTTPS hostname**. Banyan's SSRF guard (`src/lib/net/safe-fetch.ts`)
   refuses private and loopback addresses, so a Docker-internal URL such as `http://socialflow:8080` will be
   blocked unless Banyan runs with `SOCIALFLOW_ALLOW_PRIVATE_NETWORK=true`.
2. In Banyan: `SOCIALFLOW_API_URL=https://<host>` and store the API key on the workspace's SocialFlow connection.
3. Run the live smoke test (Banyan US-037): connect your own LinkedIn and X, publish one post and one 2-post
   thread, read them back with `GET /v1/posts/:id`. Use Banyan's `npm run smoke:socialflow` (`--connect linkedin|x`, then `--publish`).
4. Only then set `SOCIALFLOW_VERIFIED=true` in Banyan.

## Security notes

- The `connect_url` is a bearer capability: anyone holding it in its 30 minutes can attach *their* account to
  the organisation. Deliver it only to the signed-in founder's browser.
- API keys are shown once and stored as SHA-256 hashes. Rotate them with `keys add` and `keys revoke`.
- Provider responses are never echoed in full; errors carry a short reason at most. Tokens never appear in logs.
- No built-in rate limiting on the API: it is meant to sit behind Banyan, not on the open internet for
  arbitrary clients.

## Tests

`go test ./...` runs against fake LinkedIn and X servers: OAuth connect flows (including single-use callbacks),
publish, idempotent replay, a 3-post thread that fails at post 2 and resumes without duplicating post 1, rate
limits with `Retry-After`, a rejected token marking the account for reconnect, scheduling and its retries, revocation,
org isolation, 10 concurrent requests with one key producing exactly one post, a client hanging up mid-publish, a
connection dropped after sending, and recovery after a crash. The OAuth 1.0a signer is checked
against X's published signature example.

Every `/v1` response the tests produce has also been validated against Banyan's zod schemas
(`SFL_CONTRACT_DUMP=<dir> go test ./internal/server/` writes them; 43/43 conformed on 2026-11-01).

**Cross-repo check (2026-11-01):** Banyan's real adapter, driven by `npm run smoke:socialflow`, connected both
accounts through the browser flow and published a LinkedIn post and a 2-post X thread through this binary, against
stand-in LinkedIn and X servers. Every check passed: no duplicate on replay, and status read back as published.

**Not yet verified:** a real post on real LinkedIn and X accounts. That needs the developer apps above and is
the step that turns this from "built" into "verified".
