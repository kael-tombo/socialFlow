# Competitor and reference audit, October 17 to 24, 2026

SocialFlow.ai baseline: `04bdf9ad36d4e652224a0dd706771c9a584ac492`. Window: `2026-10-17T00:00:00Z` through `2026-10-24T23:59:59Z`, using committer timestamps on fetched default branches.

## Scope and refresh

All 39 existing reference repositories were contacted. Fetch succeeded for 38. PhotoSuite returned HTTP 403 with GitHub's DMCA takedown notice, so its current source was unavailable and was not used. Clean checkouts were fast-forwarded after restoring enough shallow history. LosslessCut could not fast-forward even after deepening; its clean checkout now selects the fetched revision detached, preserving its old local branch. Dirty checkouts were only fetched. The explicitly pinned SEO toolkit remains at v2.4.0, with newer source available through remote refs.

The fetched references contain 3,656 reachable commits in the window, including merges. This is an activity inventory with focused source investigation, not a line-by-line review of every commit. The six publishing competitors contain 107 commits; adding the 315 SocialFlow.ai baseline commits gives 422. These counts exclude open PR heads and therefore do not reproduce the supplied report's inventory methodology.

| Reference | Fetched revision | Commits | Checkout |
| --- | --- | ---: | --- |
| ace-step-webgpu | `24b978898176` | 0 | Updated |
| activepieces | `6f4be340d7c4` | 132 | Updated |
| ai-music-js | `7c445bc96ba0` | 0 | Updated |
| auto-editor | `a7409c248609` | 35 | Updated |
| beeper-bluesky | `0c2076b1b009` | 0 | Updated |
| beeper-linkedin | `c61024ede2a5` | 5 | Updated |
| beeper-mautrix-go | `3a801b425016` | 39 | Updated |
| beeper-meta | `eb0b76954836` | 21 | Updated |
| beeper-twitter | `8754d2beb8af` | 6 | Updated |
| canvas-ui | `a04a99b23529` | 0 | Updated |
| cap | `3614bdb692af` | 1273 | Local changes preserved |
| capptivo | `2ad85b0ec5a2` | 0 | Local changes preserved |
| claude-seo | `6be93f45064f` | 29 | Pinned v2.4.0; remote inspected |
| compositor | `c9c7531c620d` | 77 | Updated |
| dither-kit | `1e7faee9aa25` | 0 | Updated |
| fal-go | `8315026361e0` | 0 | Updated |
| freecut | `74d31c2524a8` | 5 | Updated |
| kdenlive | `406de22cb548` | 40 | Updated |
| losslesscut | `9537c1cf2933` | 224 | Latest detached; old branch preserved |
| memegen | `3b1cc2dc88d2` | 6 | Updated |
| miniPaint | `a79733eb803f` | 0 | Updated |
| mixpost | `df57648b8663` | 0 | Updated |
| n8n | `9a489b65ed53` | 397 | Updated |
| opencut | `e66801077856` | 0 | Local changes preserved |
| opencut-classic | `cf5e79e91914` | 0 | Local changes preserved |
| openreel-video | `367d7e90b61b` | 1 | Local changes preserved |
| openscreen | `264975d8fcd2` | 75 | Local changes preserved |
| openvid | `5f71493c55a1` | 0 | Updated |
| photosuite | Unavailable | Unknown | DMCA-blocked upstream |
| posthive | `7cdf4a0e337c` | 13 | Updated |
| postiz | `9383f5be4d31` | 75 | Updated |
| qcut | `ab389260fd5a` | 1150 | Local changes preserved |
| shotcut | `34dde4fce394` | 16 | Updated |
| shoutrrr | `83895b361508` | 10 | Updated |
| social-sdk | `72bdf668bbef` | 18 | Updated |
| socioboard | `1ec4ff5032af` | 0 | Updated |
| socioboard-6 | `e4731ec32d31` | 0 | Updated |
| trypost | `b97cf33489ba` | 9 | Updated |
| trypost-docs | `13d95e6291f3` | 0 | Updated |

## Adopted

### YouTube daily upload quotas

[Postiz #2294](https://github.com/gitroomhq/postiz-app/pull/2294), commit `30e1f83cefca`, distinguishes the daily video-insert quota from ordinary rate limits. [Google documents](https://developers.google.com/youtube/data/getting-started) a separate daily video-insert allocation.

SocialFlow.ai previously retained only HTTP 429 and the top-level numeric code. The YouTube response owner now recognizes only typed `google.rpc.ErrorInfo` details for `youtube.googleapis.com` and the exact `defaultVideoInsertPerDayPerProject` limit. It bounds parsing and discards raw messages, consumer/project IDs, and other metadata. Unknown limits and malformed bodies retain generic behavior.

The publisher reports the application's daily upload quota and asks the user to reschedule. It does not enter short automatic retries or pretend to know the reset instant. Existing failure kinds remain compatible; the error is `rate_limited` with automatic retry disabled. Regression coverage exercises the real resumable-upload entry point and separately protects publisher retry policy. The persisted media-delivery record remains recoverable after a quota rejection, so an explicit later attempt can reuse the same attachment instead of requiring replacement.

### LinkedIn native hashtags

[TryPost #446](https://github.com/trypostit/trypost/pull/446), squash `b97cf33489ba`, fixes reserved-character truncation. SocialFlow.ai already escaped those characters, but lost native hashtags. [LinkedIn's little-text grammar](https://learn.microsoft.com/en-us/linkedin/marketing/community-management/shares/little-text-format) supports explicit hashtag templates.

SocialFlow.ai now emits those templates for recognized tags, including Unicode letters and combining marks. URLs, isolated/numeric hashes, hashes inside words, and user-authored mention/template syntax stay escaped. Underscores terminate tags instead of becoming unescaped markup. Tests inspect the outgoing post request, including punctuation, Unicode, emoji boundaries, URLs, and malformed markup.

### TikTok app eligibility

[Postiz #2297](https://github.com/gitroomhq/postiz-app/issues/2297) is corroborated by [TikTok's Content Sharing Guidelines](https://developers.tiktok.com/doc/content-sharing-guidelines/). Direct Post apps must serve a broad audience; an app used only for the operator's or team's accounts is ineligible. Unaudited clients are limited to private accounts and `SELF_ONLY` posts.

The public integration guide, operator reference, and admin provider-app form now explain the restriction. The form warns when selecting TikTok, in all 12 application locales. The docs distinguish self-hosting from internal-only use, and Direct Post from inbox Upload. Saving credentials never claims provider approval.

## Independent findings and decisions

| Evidence | Current SocialFlow.ai comparison and decision |
| --- | --- |
| Social SDK `986ac2294f75`, YouTube upload `processingDetails` rejection | SocialFlow.ai already requests insertion fields separately from read-only processing status. No patch. |
| n8n `95d1b0f71e77`, retrying cancelled executions | SocialFlow.ai checks durable run state and lease identity before native effects in `services/workflows/runtime.go`. Preserve that boundary. No n8n source copied. |
| Postiz `51b47f2111c6`, Deepgram training opt-out | No Deepgram adapter found in current SocialFlow.ai. Do not add a provider-specific flag to unrelated local transcription. |
| FreeCut `74d31c2524a8`, media-library drag overlay contrast | Visual behavior reference. No equivalent defect established in SocialFlow.ai during this audit; no blanket color change. |
| Compositor `8e4b63b74aed`, non-Latin shortcuts; GIF layers and lens effects | Useful candidates, but keyboard conventions and animation scope need a separate editor acceptance case. Do not transplant Swift/native behavior. |
| Auto-editor `f4b8684db725`, non-finite action parameters | SocialFlow.ai already uses finite-value validation in its portable video contract. No identical numeric conversion bug established; not a claim that every editor action was audited. |
| OpenScreen `0b8b4065ce9c`, voiceover pieces overlapping across speed regions | Different native scene/export model. No matching SocialFlow.ai failure reproduced. Keep as a targeted future playback/export audit case. |
| Cap `104ddce7b26e`, storage endpoint restrictions | Deployment-specific SSRF policy for customer buckets. Do not blanket-block self-hosted/private storage endpoints without checking Hosted versus operator-owned semantics. |
| Beeper Meta/LinkedIn/X bridge error mapping and history cutovers | Matrix bridge/session protocols differ from the official provider contracts. No protocol or connector architecture copied. |
| Kdenlive/Shotcut render queues, selection and cross-track move repairs | Qt/MLT owners differ from browser timeline actions and durable Cloud Video Projects. No matching defect established. |
| claude-seo v2.4.3/v2.5.0 security hardening | New releases bound fetch bodies/JSON depth, block browser service workers/WebSockets, and strengthen OAuth state/PKCE. Current pin is v2.4.0. Upgrade needs compatibility/runtime verification, including Python 3.11–3.14 in v2.5.0; left as an explicit tooling decision. |

## Supplied findings checked

- TryPost [Inbox #436](https://github.com/trypostit/trypost/pull/436) is **open**, not merged. Its head is `d8f4a6d6d881`; `mergedAt` was null when inspected. Mentions and Google Business reviews are proposals, not evidence of shipped default-branch behavior.
- TikTok reconciliation already retains accepted submissions, and provider-write recovery has a 24-hour processing window and a longer inbox-delivered window. No duplicate-publish retry was added.
- Bluesky refresh already uses a bodyless POST in `platform/bluesky.go`, matching TryPost's new fix.
- Social account IDs are strings; the numeric Meta ID serialization bug does not transfer directly.
- MCP operations use registered operations and explicit dispatch; Postiz's arbitrary provider-method invocation fix does not require copying its decorator architecture.
- MCP attachment metadata and separate authored/derived media already have SocialFlow.ai owners. PHP/Laravel fixes are outside the runtime.

## Decisions left to the owner

1. Add **Mark all matching read** to Engagement? This is the strongest next product candidate. It should reuse server filters, cover unloaded pages, retain edit authorization and change-stream events, and clearly state that read status is shared within the Workspace. Current `SetEngagementState` is ID-based and appends per-item events, so this is more than a button or an unbounded SQL update.
2. Expose destination image fitting through API/MCP? Choose a bounded derivative operation and processing contract, with explicit caller selection and no replacement of originals.
3. Prioritize mentions, Google Business reviews, or a **Repeat this post** shortcut? Mentions need provider permissions, cost policy, and missing-post/webhook recovery; repeat should use existing Workflows.
4. Upgrade the pinned SEO toolkit and prioritize any image/video editor additions? These need compatibility or product decisions beyond the verified publishing fixes.

## Verification and boundaries

- Failing regressions observed before repair for LinkedIn hashtags and YouTube quota identity/retry behavior.
- `go -C apps/server test -tags dev ./internal/platform ./internal/services/publisher` passed after the final fixes.
- `bun run lint -- backend` passed with zero issues. `bun run check -- frontend` passed with zero errors and warnings. `bun run check -- ui-consistency` passed.
- `bun run build -- docs` and `bun run check -- docs` passed. The complete static-export reader suite passed, 74 browser tests.
- The existing instance-configuration component test and a temporary before/after visual harness passed, 13 tests total. Screenshots were inspected at 1440, 390, and 320 CSS pixels in both schemes, with keyboard focus on the new link, its 44-pixel minimum target, and no horizontal overflow. Temporary test/fixture files were removed from source; evidence stays under ignored `tmp/weekly-audit-visuals/`.
- Independent standards and requirements reviews found a Unicode URL-whitespace boundary and a persisted upload-recovery gap. Both were reproduced before repair and fixed with regressions. Final review results are recorded below.
- No live provider publishes, paid provider reads, production deployment, tag, push, or release were performed.
- Hindsight and Vikunja tools were not available in this session. Current source, versioned specs, official provider documentation, and the prior dated audit were used; no private task or memory write was claimed.
- Source licenses were inspected for the relevant candidates, including AGPL-3.0 Postiz/TryPost/PostHive, Apache-2.0 Shoutrrr, MIT FreeCut/Compositor/Social SDK/claude-seo, and GPL-2.0 LosslessCut. The fixes are independently implemented against provider contracts; no competitor source was vendored.

## Standards

Final source review of `04bdf9ad36d4e652224a0dd706771c9a584ac492...9ba7cbe93`: zero findings. Both prior P2 issues appear resolved. Unicode separators terminate URL tokens, and quota-rejected uploads remain recoverable while preserving explicit media classifications and existing upload state. No actionable correctness, ownership, or standards regressions found. The reviewer did not rerun tests or builds.

## Spec

Final source review of the same range: zero findings. Both prior P2 repairs are present and appear correct. The exact YouTube daily quota failure permits later recovery with the same attachment while disabling automatic retries. No actionable regressions or spec gaps found in LinkedIn encoding, bounded diagnostics, TikTok warnings, or the 12 locale additions. The reviewer did not independently rerun supplied validation.

Final review totals: standards 0, requirements 0.
