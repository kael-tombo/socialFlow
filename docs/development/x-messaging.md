# X messaging authorization and event ingestion

X legacy DMs can use a supplemental OAuth2 user grant while publishing keeps its existing OAuth1 grant. Encrypted XChat is unsupported. This document is for operators configuring X credentials and engineers wiring Inbox ingestion.

## Configure the supplemental app

Keep the existing X consumer key and consumer secret unchanged. Enable OAuth2 for the same X application, obtain its OAuth2 client ID and client secret, and register an exact callback URI. The OAuth2 ID and secret are different credentials from the OAuth1 consumer key and secret.

`config.LoadXMessaging()` reads these dedicated settings, including their normal `_FILE` secret alternatives:

| Setting                                                  | Default | Purpose                                                                  |
| -------------------------------------------------------- | ------- | ------------------------------------------------------------------------ |
| `OPENPOST_X_MESSAGING_CLIENT_ID`                         | Empty   | OAuth2 client ID                                                         |
| `OPENPOST_X_MESSAGING_CLIENT_SECRET`                     | Empty   | Confidential client authentication and webhook signature verification    |
| `OPENPOST_X_MESSAGING_REDIRECT_URI`                      | Empty   | Exact registered callback URI                                            |
| `OPENPOST_X_MESSAGING_WEBHOOK_ID`                        | Empty   | Existing, verified X webhook registration                                |
| `OPENPOST_X_MESSAGING_MONTHLY_BUDGET_MICROUSD`           | `0`     | Per-Workspace monthly XAA admission limit, including reserved exposure   |
| `OPENPOST_X_MESSAGING_PROVIDER_SPENDING_LIMIT_CONFIRMED` | `false` | Operator acknowledgment that external X spending controls are configured |

Zero budget disables subscription activation. The existing X publishing budget does not enable messaging events. Configure X's own credit and spending controls before acknowledging them. SocialFlow.ai does not create a webhook registration or activate a subscription during OAuth authorization, account reads, startup, or Inbox navigation.

`Begin` authorizes the actor at `LevelConnect`, stores a ten-minute, single-use state bound to the actor, Workspace, social account and client ID, and requests `tweet.read users.read dm.read dm.write offline.access` with S256 PKCE. The verifier, access token, refresh token and webhook payloads are encrypted at rest. Callback completion verifies `/2/users/me` against the already-connected X account and rechecks access before storing the separate grant. Neither `social_accounts` credential mirrors nor `oauth_grants` are changed. Refresh ownership is leased in the database so concurrent workers cannot rotate the same refresh token.

The configured callback must hand its `state` and `code` to the authenticated completion endpoint as the initiating user. For browser clients this may be a session-authenticated callback bridge; native clients must finish through their authenticated API session. Never put an SocialFlow.ai bearer credential in the callback URL. The callback integration belongs to the consuming client, and the service does not accept an unauthenticated user ID supplied by X.

## Wire the server owners

Migration `167_x_messaging_oauth2.sql` is additive and works with SQLite and PostgreSQL. It adds separate grant, PKCE request, quota, subscription and encrypted receipt tables. Disabling the feature leaves OAuth1 publishing intact. Do not drop these tables while subscriptions or unreconciled reservations exist.

1. Load `config.LoadXMessaging()` and construct `oauth2messaging.Config` from its fields. Pass the database, shared token encryptor and existing `usage.Service` to `oauth2messaging.NewService`.
2. Before `usageService.SetProviderCostPolicy`, wrap the existing policy with `oauth2messaging.AddPricing(policy, messagingBudget)`. This adds `x_activity` accounting with the documented inbound price and preserves the `x` publishing policy. Install the policy in either edition when messaging subscriptions are configured.
3. Set the messaging service's supplemental token source to this service. `AccessToken(ctx, workspaceID, socialAccountID)` returns an already-tagged adapter token. Only `errors.Is(err, oauth2messaging.ErrNotConnected)` permits OAuth1 fallback. Expired grants, refresh contention, invalid grants, missing scopes and changed X identities must retain their errors.
4. Call `xAdapter.SetMessagingQuotaGate(service)` on every X adapter before serving requests. Both OAuth1 and OAuth2 messaging calls use the same durable quota owner. Publishing does not consume messaging quotas.
5. Register `handlers.NewXMessagingHandler(authenticator, service).RegisterRoutes(api)` and `.RegisterWebhook(echoServer)`. Webhook GET and POST use `/api/v1/webhooks/x/messaging`. POST verifies `X-Twitter-Webhooks-Signature-OAuth2` against raw bytes using the OAuth2 client secret, without fallback to an OAuth1 secret.
6. Set `service.SetReceiptScheduler(func(context.Context) error)` to enqueue an immediate, deduplicated durable drain job after receipt commit. Return enqueue failures so X can retry. The regular durable Inbox sweep must also drain receipts, covering a process failure between commit and enqueue. Do not launch an in-memory goroutine for receipt work.
7. Drain through `service.Drain(ctx, apply, 100)`. The callback receives `(context.Context, socialAccountID, platform.FetchMessagesResult)`. Wrap `batch.Conversations` in `platform.FetchMessageEventsResult` and call `messaging.Service.ApplyProviderEvents`. An empty `NextCursor` must preserve the account's existing log cursor. The core owner atomically applies messages and its client-facing change records. Remote message IDs make apply retries idempotent. Failed receipts receive a durable retry delay of 30 seconds, increasing to at most ten minutes. A failed account does not block other receipts or subscription maintenance. Shutdown failures also retain retryable state and do not prevent other accounts from stopping.

The dedicated API routes are:

- `GET /accounts/{account_id}/messaging/x?workspace_id=...`, stored readiness, with no provider calls.
- `POST /accounts/{account_id}/messaging/x/authorize`, body `{workspace_id}`.
- `POST /accounts/x/messaging/complete`, body `{state, code}`.
- `POST /accounts/{account_id}/messaging/x/subscriptions`, body `{workspace_id, event_type, event_limit}`, Workspace administration required.
- `DELETE /accounts/x/messaging/subscriptions/{subscription_id}`, body `{workspace_id}`, Workspace administration required.

Paths above are relative to `/api/v1`. REST API token exposure requires the normal operation-catalog review; these new operations do not implicitly expand delegated application scopes. Session and CLI authentication retain the existing middleware policy.

## Activate and stop event delivery deliberately

Register and verify the HTTPS webhook in X's console or official API separately, then supply its ID. Creating a subscription requires a positive explicit budget, the external spending-control acknowledgment, a valid supplemental grant, and a webhook signing secret. Activation is an explicit operator action with a reserved allowance of 1 to 100 events and an expiry of 15 minutes. Only `dm.received` and `dm.sent` are supported. No encrypted `chat.*` subscription is created.

For inbound events, SocialFlow.ai reserves every allowed event at **USD 0.01 per event** through the existing cost owner before creating the subscription. An insufficient or absent priced policy prevents upstream activation. Each accepted delivery consumes one reserved slot. Successful delivery accounting uses `event_uuid`; remote message IDs reconcile sent echoes with REST/send results. `dm.sent` is not billed by the current event-pricing table.

The allowance is a local admission limit, **not a cap on X's bill**. X's subscription API accepts an expiry, not a maximum event count. Network failures, queued delivery, a burst before deletion and external traffic can exceed local allowance. Expiration bounds duration; provider console controls bound external spending according to X's actual terms. Receipts beyond the allowance return a retryable failure and are not silently imported. Operators must reconcile unexpected external usage with X.

Explicit authorization or validation rejections with HTTP 401, 403 or 422 release unspent exposure and permit retry. Ambiguous activation, including conflicts and uncertain HTTP 400 responses, retains reserved exposure and blocks duplicate activation for that account/event. Inspect the X console before retrying. Known subscriptions can be explicitly deleted; a successful HTTP status alone is insufficient. SocialFlow.ai requires `data.deleted: true` without structured errors before marking one stopped. `Drain` also calls `Maintain`, which removes expired PKCE requests and retries stopping exhausted or expired known subscriptions. Unused reservations stay conservative after deletion because queued delivery and billing can outlive subscription deletion. Reconcile the provider invoice before releasing unused exposure through the existing usage owner. A subscription marked unknown requires operator reconciliation, including its remote ID, before another activation.

## Understand readiness and REST limits

`Readiness` reads stored grant state. It reports missing authorization, changed app/identity, invalid grants and missing operator budget without contacting X. It never declares encrypted XChat support. A valid grant is not proof of provider entitlement or successful subscription activation, and send requests remain subject to X's real permission and rate-limit responses.

The durable quota owner reserves before each X messaging request and retains ambiguous requests. DM lookup and send each allow at most 15 requests per rolling 15 minutes per remote user. Send admission also reserves the documented daily 1,440 per-user and per-app allowance. Participant hydration is separately limited to one request per rolling 15 minutes to avoid spending on repeated profile lookups. Provider exhaustion/reset headers and 429 responses can extend these limits across restarts. A quota observation failure preserves an already-confirmed send ID and blocks further adapter requests until the pending observation can be stored; the pre-request reservation remains durable. These are conservative local protections; traffic outside SocialFlow.ai can consume the same provider quota.

## Verify without activating a paid API

Run the focused service, adapter, configuration and encryption-rotation tests. They use isolated SQLite databases and local HTTP fixtures. The tests cover real PKCE exchange, wrong provider identity, unchanged OAuth1 storage, encrypted supplemental credentials, one refresh owner, missing-grant fallback, invalid-refresh handling, quota persistence and boundaries, pre-activation budget admission, signed receipts, queue failure/replay and encrypted-XChat rejection. They never activate an external subscription.

Set `OPENPOST_TEST_POSTGRES_URL` to a disposable local PostgreSQL database to run the service suite against isolated temporary schemas and the full production migrations. Integration verified these service behaviors on PostgreSQL as well as SQLite. Live provider entitlement, consent, registered webhook delivery and actual billing require an authorized installation and explicit operator budget; local fixtures do not establish them.

## Official sources

Reviewed 20 October 2026:

- [OAuth2 authorization code with PKCE](https://docs.x.com/fundamentals/authentication/oauth-2-0/authorization-code)
- [X Activity privacy and expiration](https://docs.x.com/x-api/activity/introduction)
- [Create an X Activity subscription](https://docs.x.com/x-api/activity/create-x-activity-subscription)
- [Delete a subscription and confirm its result](https://docs.x.com/x-api/activity/deletes-x-activity-subscription)
- [Event payloads, including legacy DMs and encrypted XChat](https://docs.x.com/x-api/activity/event-payloads)
- [Event pricing](https://docs.x.com/x-api/activity/event-pricing)
- [Webhook signatures and CRC](https://docs.x.com/x-api/webhooks/introduction)
- [REST rate limits](https://docs.x.com/x-api/fundamentals/rate-limits)
