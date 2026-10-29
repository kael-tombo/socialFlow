# Competitor audit, October 10 to 17, 2026

This audit checked the supplied weekly report against the refreshed reference checkouts and the SocialFlow.ai owners affected by each claim. It uses commits dated from `2026-10-10T00:00:00Z` through `2026-10-17T23:59:59Z` on each remote default branch.

## Inventory

The refreshed branches contain 354 commits in the date window, not 366:

| Repository | Branch | Revision | License | Commits |
| --- | --- | --- | --- | ---: |
| SocialFlow.ai | `origin/main` | `c92f2cb03577` | AGPL-3.0 | 226 |
| Postiz | `origin/main` | `08b1e6b46f49` | AGPL-3.0 | 73 |
| PostHive | `origin/main` | `7d70b489344f` | AGPL-3.0 | 34 |
| TryPost | `origin/main` | `b06344b56dea` | AGPL-3.0 | 20 |
| Shoutrrr | `origin/main` | `2abb80965313` | Apache-2.0 | 1 |
| Mixpost | `origin/main` | `df57648b8663` | MIT | 0 |
| Socioboard | `origin/master` | `1ec4ff5032af` | GPL-3.0 | 0 |

The count is only an inventory. Each decision below comes from the relevant diff, its tests, SocialFlow.ai's current owner, and provider documentation when available.

## Changes adopted

### Instagram publication rules

TryPost `0d717d839135` records successful collaborator tests for feed posts, carousels, and Reels. Its Story test kept stale collaborator data but omitted it from the provider request. Trial Reels rejected collaborators.

SocialFlow.ai now:

- caps collaborators at three in the capability contract;
- rejects Trial Reel and collaborator combinations before provider I/O;
- rejects Trial Reel settings outside the Reel format;
- omits stale collaborators from Story requests instead of blocking an existing draft or scheduled post.

The Story behavior avoids repeating the hidden-setting regression reported in GitHub issue #299.

### TikTok photo dimensions

Postiz `a15f6ea3023b` records a `2172x2896` failure and a successful `1080x1440` replacement. [TikTok's media transfer guide](https://developers.tiktok.com/docs/en/content-posting-api-media-transfer-guide) says "Maximum 1080p" but does not define the dimension. The shorter-side rule is operational evidence from Postiz, not an explicit provider contract.

SocialFlow.ai now exposes `max_short_side` in media capabilities, sets it to 1080 for TikTok photo posts, carries stored dimensions into the adapter request, and rechecks the rule before provider I/O. The shared limit owner records both the official documentation and the operational interpretation. Unknown dimensions remain allowed so old media does not become unpublishable.

### Instagram container retries and diagnostics

Postiz `b95e1e2cb4a4` retries the exact `9004/2207052` failure at media-container creation. SocialFlow.ai now uses its existing bounded, cancellation-aware Meta retry helper only for that operation and exact pair. It does not retry carousel-parent creation, publication, transport failures, or generic `9004` responses.

SocialFlow.ai also maps fixed, provider-text-free guidance for:

- `110/2207018`, an ineligible collaborator;
- `20/1772107`, a rejected first comment after the parent post published;
- `2207053`, a terminal Instagram video-limit rejection.

### Facebook regular-video views

Postiz `b64b2c004822` replaced the invalid `total_video_impressions` metric with `total_video_views`. SocialFlow.ai now requests `total_video_views` for regular videos while retaining `fb_reels_total_plays` for Reels. Keeping the two formats separate prevents duplicate view totals if Meta returns both metrics.

### YouTube thumbnail recovery

The supplied report's partial-success recommendation was stale. SocialFlow.ai has retained the uploaded video ID and public URL across thumbnail failures since `8e35407eb364`, with regression coverage for retries that must not upload the video again.

The remaining gap was the phone-verification explanation from Postiz `b2897ff7d909`. SocialFlow.ai now recognizes the endpoint-specific `youtube.thumbnail` error domain and stores fixed guidance to verify the channel at `youtube.com/verify`. Other thumbnail failures keep their existing classification.

### Google OAuth scope ownership

TryPost `7579ec356ae5` removed `include_granted_scopes=true` from YouTube and Google Business authorization. [Google documents](https://developers.google.com/identity/protocols/oauth2/web-server) that this flag adds all scopes previously granted to the application to the new token. SocialFlow.ai used the flag in both adapters even though each requests its full provider-specific scope set. SocialFlow.ai now omits it, which prevents a reused Google OAuth client from merging unrelated previous grants into the new token.

## Confirmed but deferred

### ChatGPT MCP sign-in

TryPost `04f3355ae16f` and [OpenAI's tool-auth contract](https://developers.openai.com/plugins/build/auth) confirm the interoperability gap. SocialFlow.ai returns an HTTP 401 before JSON-RPC dispatch. ChatGPT can also request sign-in from a successful tool error result through `_meta["mcp/www_authenticate"]`.

This needs a separate compatibility change. A safe implementation must distinguish a verified ChatGPT network request, allow only anonymous discovery methods, keep workspace handlers unreachable, preserve the current 401 flow for Claude and generic MCP clients, and pass a real ChatGPT install test. User-Agent matching or a fake authenticated principal would weaken the boundary.

### Dynamic OAuth registration behind proxies

TryPost `04a502ddeadb` led to a real SocialFlow.ai code finding: dynamic client registration keys its budget from `RemoteAddr`, while trusted proxy middleware resolves and sanitizes the canonical client IP. Clients behind one reverse proxy therefore share a budget.

The pasted report overstated the current effect. Dynamic registration defaults off, and Hosted advertises Client ID Metadata Documents instead of a registration endpoint. The repair still needs a provenance-marked client IP in request context, an untrusted-header regression test, and a separate global registration budget. It should not exempt OpenAI or Anthropic address ranges from durable client creation limits.

### Product additions

Instagram music attachment and LinkedIn company mentions require new permissions, account capability discovery, product controls, and live provider certification. They are not adapter-only changes. This audit did not add them.

PostHive billing gates, Shoutrrr free-account request limits, Accept-header normalization, HubSpot, WebDAV, and Mastra transport work either already have stronger SocialFlow.ai owners or do not fit SocialFlow.ai's product model.

## Regression review

GitHub issue #342 was real, but its nil-map mutation path predates the October 10 audit. The MCP media schema landed in July, the publisher mutation in July, and null media settings writes in September. Current main fixes the owner boundary in `a307c1342` and related null mutation paths in `8bf046929`. No additional #342 change came from this audit.

GitHub issue #340 is also fixed on current main in `8895f88b`. The LinkedIn organization lookup now recovers when an app rejects optional projection fields.

The adopted changes have focused tests at the capability, adapter, error-classification, analytics, OAuth URL, and publisher failure boundaries. No live social-provider post, paid API read, deployment, or Hosted verification was performed.
