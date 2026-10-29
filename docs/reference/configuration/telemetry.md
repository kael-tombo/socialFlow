# Product telemetry and error tracking

SocialFlow.ai can send privacy-limited product, service, website, documentation, and error events to PostHog. The Hosted service enables this integration. Self-hosted installations leave it disabled unless their operator explicitly configures an operator-owned PostHog project.

Separately, self-hosted instances send privacy-limited maintainer diagnostic reports to SocialFlow.ai so crashes get fixed everywhere. See [Maintainer diagnostics](diagnostics.md).

SocialFlow.ai uses three PostHog SDKs because they observe different facts:

- `posthog-js` records browser page views, explicit product interactions, and sanitized browser failures.
- `posthog-go` records authoritative server outcomes, background-job failures, and sanitized backend exceptions.
- `posthog-react-native` records consented screen visits, selected actions, and sanitized JavaScript failures in the native app.

Do not record the same outcome in both layers. Browser events describe intent, such as `publication publish requested`; server events describe confirmed outcomes, such as `rendition published`.

The first-use journey records these milestones. Different users can reach them in different orders:

1. `signup started`: browser submission intent
2. `signup completed`: server account creation
3. `plan confirmed`: server purchase confirmation
4. `workspace created`: server first Workspace creation
5. `checkout completed`: server confirmation of the consumed successful checkout return
6. `destination connected`: server claim of the first connected destination
7. `first composition started`: server-owned observation after the server accepts the first meaningful composition claim
8. `workspace activated`: server exactly-once Activation transition

The browser must not duplicate authoritative server outcomes. Funnel events accept only the properties declared in the shared browser and backend telemetry catalogues. Unknown event names, unknown properties, email addresses, secret-bearing URLs, and credential-shaped values are rejected before enqueue.

## Privacy boundary

The shared browser client has four states:

- **Undecided:** no optional browser telemetry is sent.
- **Persistent:** after the visitor allows analytics cookies, PostHog keeps first-party identity and sessions across SocialFlow.ai sibling subdomains. The signed-in app identifies the browser only with the opaque SocialFlow.ai user ID. PostHog derives country and bot signals before discarding raw client IP.
- **Cookieless:** after the visitor continues without analytics cookies, PostHog receives limited personless events with daily server-hashed identity, memory-only state, no geography, and no reliable multi-day retention.
- **Off:** no optional browser telemetry or browser correlation headers are sent. Do Not Track and Global Privacy Control force this state.

The preference is separate from mandatory policy acknowledgement and can be changed from **Analytics choices** in each browser surface. The Hosted service shares that choice through a first-party `openpost_analytics` cookie scoped to `.openpo.st`. Self-hosted deployments use a host-only cookie. Changing from an initialized mode clears the current origin's PostHog state and reloads; another sibling origin clears its own local state when it next loads.

Browser autocapture, session replay, surveys, console capture, network timing and bodies, heatmaps, dead-click capture, and automatic browser exception capture are disabled. SocialFlow.ai records manual page views and matching page leaves. It records CLS, FCP, INP, and LCP without element attribution. Persistent marketing and documentation events remain anonymous until a user signs into the app. Cookieless events always remain personless.

Backend product and service telemetry is a separate observation boundary. It records authoritative outcomes with opaque identifiers and keeps Go SDK GeoIP disabled. Browser analytics choices do not suppress these backend events, and cookieless browser requests do not send correlation headers that could join them to service outcomes.

Do not add email addresses, names, usernames, post text, media, request bodies, credentials, query strings, or raw URLs to events or exceptions. Use route templates, stable object IDs, status codes, release identity, and enumerated failure types. A PostHog project token is write-only and may be exposed to the browser; a personal API key must remain in CI secret storage.

`first composition started` is a server-owned observation recorded only after the server atomically accepts the first meaningful composition for a Workspace. Its backend property allowlist contains only `signal`, with one of `text`, `media`, or `content_mode`. It excludes authored content, prompts, captions, media URLs, identity data, provider handles, destination identifiers, and secret-bearing URLs. Opening or focusing the composer, selecting a destination, and saving an empty draft do not emit it. The browser never captures this milestone; unrelated optional editor-intent events are unchanged.

Before enabling telemetry, configure the PostHog project to:

- use stateful cookieless server hashing for the cookieless browser mode;
- derive approved country and bot properties for persistent browser events, then discard raw client IP;
- remove city, region, latitude, longitude, postal code, and accuracy-radius properties after enrichment;
- retain events for no more than 12 months;
- keep broad autocapture, automatic browser exception capture, replay, heatmaps, surveys, dead clicks, console capture, and network payload capture disabled;
- enable Web Vitals for CLS, FCP, INP, and LCP while leaving network timing and element attribution disabled;
- use the EU Cloud project when operating the official Hosted service.

Changing that boundary requires a privacy review and a matching policy update before deployment.

## Native collection and delivery

Native analytics stays off until the user chooses **Allow analytics**. The choice is stored separately for each server on the device and can be changed in **Privacy**. Changing server or actor stops correlation immediately; a consent dialog from a previous server cannot enable the new server. The native client uses a bounded memory queue, opaque signed-in user identity, known screen templates, and allowlisted technical properties. It strips raw error messages and keeps only compiled app stack locations. It does not record touch events, replay, console logs, network bodies, or native crashes.

Browser runtime configuration and SDK loading use bounded retries. Native configuration retries when the app returns to the foreground after a failed load. Capture failures must not interrupt publishing or editing. Consent decline clears pending optional events.

Server errors include committed Huma 5xx responses and terminal worker failures, including failures whose database status update also fails. `api mutation completed` measures accepted or rejected REST write responses by route template and status class. A successful HTTP response is not evidence that a background job or publication succeeded.

Video project creation and export outcomes, documentation search result counts and successful code copies, accepted AI builds, and terminal Workflow runs have explicit events. Search text, generated content, and authored Workflow definitions stay out of telemetry.

## App and backend configuration

Set these values in the process environment or in **Settings → Instance → Configuration**. Configuration changes take effect after restart.

```dotenv
OPENPOST_TELEMETRY_ENABLED=true
OPENPOST_POSTHOG_PROJECT_TOKEN=phc_operator_owned_project_token
OPENPOST_POSTHOG_API_HOST=https://eu.i.posthog.com
OPENPOST_POSTHOG_BROWSER_HOST=https://cool.openpo.st
OPENPOST_POSTHOG_UI_HOST=https://eu.posthog.com
OPENPOST_TELEMETRY_ENVIRONMENT=production
```

`OPENPOST_POSTHOG_API_HOST` is the direct server-side ingestion endpoint. `OPENPOST_POSTHOG_BROWSER_HOST` may be a first-party reverse proxy. Cloud mode defaults to the Hosted service `https://cool.openpo.st` proxy; self-hosted deployments fall back to the direct API host unless the operator sets a proxy. A browser proxy must forward PostHog ingestion and static asset paths, support `GET` and `POST`, preserve the client connection metadata needed for the approved PostHog country and bot transformations, and avoid forwarding SocialFlow.ai `Cookie`, `Authorization`, and unnecessary `Referer` headers. Keep `OPENPOST_POSTHOG_UI_HOST` set to the real PostHog UI host.

The browser reads its safe runtime configuration from `GET /api/v1/telemetry/config`. This lets one static SocialFlow.ai bundle work in the Hosted service and in self-hosted installations without compiling the Hosted service token into every binary.

## Marketing and documentation

The marketing and documentation sites are separate static builds. Give both builds the same production project and environment as the app:

```dotenv
VITE_POSTHOG_PROJECT_TOKEN=phc_operator_owned_project_token
VITE_POSTHOG_API_HOST=https://cool.openpo.st
VITE_POSTHOG_UI_HOST=https://eu.posthog.com
VITE_OPENPOST_ENVIRONMENT=production
VITE_OPENPOST_VERSION=3.4.0
VITE_OPENPOST_REVISION=<git-revision>
```

Use one PostHog production project across the app, backend, marketing site, and documentation so acquisition and product adoption can be analyzed together. Use separate projects for development and staging. Every event includes a stable `surface` and environment so queries can still separate them. Production public-site builds stop with an error when the project token, exact Hosted service proxy host, or EU UI host is missing.

The browser rewrites PostHog-owned page lifecycle and Web Vitals URL properties to the route template already captured by SocialFlow.ai. It removes query strings, fragments, raw dynamic route values, referrer paths, page titles, resource entries, and element attribution before delivery. Every browser event carries `analytics_mode` so reports can separate persistent identities from daily cookieless hashes.

## Source maps

Production source-map upload is opt-in in CI. Set `POSTHOG_SOURCEMAPS_ENABLED=1`, then provide `POSTHOG_PERSONAL_API_KEY`, `POSTHOG_PROJECT_ID`, `POSTHOG_UI_HOST`, and a release identity through `OPENPOST_RELEASE_VERSION`, `GITHUB_SHA`, or Cloudflare's automatic `CF_PAGES_COMMIT_SHA`. The personal API key is a CI secret and must never use a `VITE_` prefix. Builds upload hidden source maps and delete them from the deployed output after upload.

For the Hosted public site, open Cloudflare's `openpost-marketing` Pages project, then **Settings → Variables and Secrets → Production**. Add `POSTHOG_PERSONAL_API_KEY` as an encrypted secret, using a PostHog personal API key restricted to the production project with Error Tracking write access. Add `POSTHOG_SOURCEMAPS_ENABLED=1`, `POSTHOG_PROJECT_ID=246054`, and `POSTHOG_UI_HOST=https://eu.posthog.com`. Keep the existing `VITE_POSTHOG_*` collection settings. Save, then retry the latest production deployment. Its build log must show successful source-map uploads for both marketing and docs before treating public-site errors as symbolicated. Do not change the Preview environment or put the private key in a browser-prefixed variable.

For the Hosted application, keep the same private key in the GitHub repository's **Settings → Secrets and variables → Actions → Repository secrets**, named `POSTHOG_PERSONAL_API_KEY`. A listed secret name does not prove the running build receives a nonempty key. Confirm `POSTHOG_SOURCEMAPS_ENABLED=1` and successful uploads in the canonical frontend build log, without printing the key. Publish a new app release after restoring the credential so production uses that mapped build; do not replace an existing release tag or image.

## Shutdown and delivery

SocialFlow.ai creates one backend PostHog client at startup and closes it during graceful shutdown so queued events can drain. Event delivery is best-effort and must never decide authorization, billing access, publication state, or job success. Keep business records in the SocialFlow.ai database; telemetry is an observation layer, not a source of truth.

## Production reports and verification

`config/posthog-analytics.json` owns the prepared production URLs, report definitions, exclusions, and conversion goals. Saved reports use canonical event names with spaces, production-only filters, test-account exclusions, and explicit smoke/internal flags. Public traffic and authenticated product activity have separate scopes. Active Workspace reports count distinct `workspace_id` values; publishing retention measures returning identified people.

The primary **Workspace activation** report joins `workspace created` and `workspace activated` on `workspace_id`, so a teammate can activate a Workspace created by another user. It measures selected creation cohorts and activation within 30 days; recent cohorts are still maturing. The **Workspace setup milestones** report measures destination connection and meaningful composition independently so alternate valid journeys remain visible. Waitlist, signup, and activation goals measure server-confirmed outcomes.

Consented signed-in identities can connect activity across devices. Cookieless events cannot establish reliable multi-day retention. Hosted administrator identities receive the internal/test marker when identified; historical unmarked internal visits cannot be reliably reconstructed from these filters alone.

After applying configuration, read back every saved definition and verify representative results in PostHog. After deployment, check current release properties, known page and screen paths, conversion event names, and sanitized error properties. Synthetic verification events must carry `openpost_smoke = true` and remain excluded from production reports.

## Journey diagnostic

An optional eight-step ordered funnel can diagnose a particular signup journey using the milestone list above. It is not the primary activation denominator because checkout and destination setup can happen in different orders. Keep any personless diagnostic sequence marked as smoke traffic and verify its exclusion before interpreting production totals.

See PostHog's [data-collection controls](https://posthog.com/docs/privacy/data-collection), [Go SDK guide](https://posthog.com/docs/libraries/go), [proxy guide](https://posthog.com/docs/advanced/proxy), and [GDPR guidance](https://posthog.com/docs/privacy/gdpr-compliance) for provider-side setup.
