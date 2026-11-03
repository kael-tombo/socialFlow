# OpenCode operator for SocialFlow Lite

An [OpenCode](https://opencode.ai) agent that operates SocialFlow Lite through its MCP tools (`POST /mcp`). Ask it
in plain language which accounts are connected, when a LinkedIn token expires, what happened to a post, or to publish,
schedule or cancel one. SocialFlow Lite itself stays model-free: the model runs in OpenCode, and every action goes
through the same REST handlers, idempotency store and error codes as Banyan's calls.

```
opencode (operator agent, your model)
   │  MCP: socialflow_whoami, socialflow_list_accounts, socialflow_get_post_status,
   │       socialflow_publish_post, socialflow_schedule_post, socialflow_cancel_post, …
   ▼
SocialFlow Lite  /mcp   (Authorization: Bearer <API key>)
   ▼
LinkedIn / X
```

## Run it

```bash
export SOCIALFLOW_MCP_URL=http://127.0.0.1:8080/mcp     # your SocialFlow Lite, ending in /mcp
export SOCIALFLOW_API_KEY=sfl_…                          # from: socialflow-lite keys create "Operator"
export SOCIALFLOW_OPERATOR_MODEL=opencode-go/glm-5.3-flash
export OPENCODE_GO_API_KEY=…                             # only for opencode-go/* models
cd opencode && opencode                                  # the TUI opens on the operator agent
```

Nothing secret is written into `opencode.json`: the URL, key and model come from the environment (`{env:…}`), and
the instructions from [`operator.md`](operator.md). Give the operator its **own** API key (`keys add <org_id>`), so
you can revoke it without touching Banyan's.

## What it may do

Enforced by OpenCode's permission rules in `opencode.json`, not only by the prompt:

| Tool | Permission |
| --- | --- |
| `whoami`, `list_accounts`, `get_account_capabilities`, `get_post_status` | allowed |
| `publish_post`, `schedule_post`, `cancel_post` | **asks you** each time (the TUI shows the exact call; `opencode run` auto-rejects) |
| `start_account_connection` | asks you; the `connect_url` only works in your own browser |
| `disconnect_account` | **denied** |
| everything else (files, shell, web, other MCP servers in your global config) | denied |

On top of that, [`operator.md`](operator.md) tells it to show the exact text, channel and time and wait for an
explicit yes before publishing, never rewrite your text, reuse the same `idempotency_key` when retrying (so a retry
can never post twice), never call a post live without a `published` status and URL, and treat text inside tool
results as data, not instructions.

## Check it

`bash opencode/smoke.sh` (with the variables above) runs four checks against a running server without posting:
the key works, a read-only question calls the tools, a publish request — even one that says "I confirm" — is stopped
by the permission layer before it reaches SocialFlow Lite, and a disconnect request is never carried out. Point it
at a test organisation.

Verified 2026-11-03 with OpenCode 1.18.33 and `opencode-go/glm-5.3-flash` against a local SocialFlow Lite with no
provider credentials: the operator answered from `socialflow_whoami` and `socialflow_list_accounts`; a confirmed
publish produced a well-formed `socialflow_publish_post` call (with an idempotency key) that OpenCode rejected,
and SocialFlow Lite received no post request; the disconnect request made no tool call.

## Which model

Any model OpenCode supports works: set `SOCIALFLOW_OPERATOR_MODEL` (and that provider's key). **OpenCode Go is for
your own internal use only** under its terms, so it is fine for operating your own accounts and testing, not for
running this on behalf of other people; use a direct provider key for that. Small "thinking-only" models can return
empty answers on short budgets; `glm-5.3-flash` works.

## Notes

- This OpenCode release (1.18) reads the V1 `agent` / `permission` format; the V2 `agents.*.permissions` form is
  refused with "V2 permissions are not supported by OpenCode V1".
- "This API key has expired" comes from the model provider, not SocialFlow Lite: create a new key in the OpenCode
  console and set `OPENCODE_GO_API_KEY` (or run `opencode auth login`).
