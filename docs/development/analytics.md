# Analytics Architecture

Analytics is an optional platform feature, not part of the core publishing adapter. Publishing, analytics, comments, and inboxes use separate interfaces so a network can support only the features its API allows.

This page is for contributors changing analytics collection, storage, or presentation boundaries.

## Boundaries

`platform.AnalyticsAdapter` is optional and declares account and SocialFlow.ai-managed content support plus required scopes. Its methods return normalized counters. Missing keys mean “not reported”; zero means the provider returned a measured zero. Every metric carries a unit, aggregation meaning, provider source, and reporting period where applicable.

SocialFlow.ai only tracks content published through SocialFlow.ai. There is no inventory for content published elsewhere: no provider history reads, no discovery jobs, and no external analytics sources. The publishing `platform.Adapter` remains unchanged. `platform.CommentAdapter`, `platform.EngagementAdapter`, and `platform.MessagingAdapter` stay separate from analytics. Each read path has its own access checks, cursors, data limits, and audit records.

## Persistence

- `analytics_account_snapshots` stores immutable normalized account measurements.
- `analytics_rendition_snapshots` stores one total per published Rendition. Thread segment IDs are collected together and SocialFlow.ai's own replies are removed from reply totals.
- `analytics_sync_states` stores the latest metrics, collection status, safe error code, last attempt and success, next due time, and unchanged streak.

Snapshots include workspace and provider identity for scoped queries and deletion. Raw provider payloads, remote media, access tokens, bot credentials, and webhook secrets are intentionally discarded.

## Jobs and cadence

The worker handles `analytics_sweep`, `analytics_account_sync`, and `analytics_rendition_sync`. A partial unique job index prevents duplicate active work for the same subject. The sweep runs every 15 minutes, queues only due subjects, and always schedules its successor.

Explicit Workspace refreshes can collect published renditions of any age through the existing permission, readiness, deduplication, and rate-limit gates. Background collection retains its seven-day eligibility window.

Account checks start on a daily cadence. Rendition checks use 1, 3, 12, and 24 hour bands until day 7. Repeated unchanged results can increase the current interval to 8× its base cadence. Stored backoff and `Retry-After` apply to scheduled and manual refresh alike.

Permission and unsupported states do not consume provider quota. SocialFlow.ai re-evaluates them during sweeps so reconnects and newly added adapter support recover without manual database changes. Rate limits preserve the last successful values and use the provider's retry hint when available.

The sweep uniqueness rule applies to pending sweeps. A running sweep can queue its successor, and worker recovery marks a stale running sweep complete when that successor already exists. Do not broaden the sweep index to pending and processing rows without changing this lifecycle.

## API and UI

`GET /api/v1/analytics` reads stored data only. The account, sort, range, and offset are bound into the opaque next cursor. It returns flat content references for SocialFlow.ai publications, metric metadata, and server-owned insights calculated before content pagination. Account growth remains explicitly account-wide. Page reads never call a provider.

`POST /api/v1/analytics/refresh` requires editor access and reconsiders collection without bypassing provider gates. `POST /api/v1/analytics/repurpose` accepts a Workspace ID, discriminated content reference, and range in the request body. It recomputes bounded evidence and returns a fresh local composer handoff without writing a Publication, changing the source, or calling an AI provider.

The API preserves engagement and exposure types. The frontend and mobile declarations are regenerated from Huma through OpenAPI. Analytics is intentionally not a first-class CLI or MCP workflow; their parity documentation continues to mark it unavailable rather than publishing a divergent contract.

## Reporting windows

Overview, exports, and repurpose evidence share the same range contract. `days` accepts 7, 30, 90, 365, or 0 for all time. Existing requests still default to 30 days. A custom range supplies both `from` and `to` as inclusive UTC calendar dates and takes precedence over `days`. The server resolves an inclusive start and exclusive end, caps the interval at the current time, and rejects incomplete, inverted, or invalid dates. Pagination binds the requested dates as well as the preset and account.

All time begins at the earliest stored published post or analytics snapshot in the Workspace. It does not initiate provider history reads. Published-content inventory follows its original publication time; trends include only measurements captured within the selected interval. Lifetime counters and account-wide current measurements retain their existing scope labels, so choosing a historical interval never relabels a lifetime total as activity during that interval. Exports use the resolved interval, and the web URL preserves the selected preset or custom dates.
