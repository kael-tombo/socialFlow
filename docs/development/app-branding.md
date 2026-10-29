# App branding

App branding changes the name and artwork inside an authenticated SocialFlow.ai workspace. It is independent of the theme library, organization/workspace names and avatars, and the content-creation Brand kit.

## Configure branding

Organization owners and admins use **Settings → Organization → Appearance → App branding** to set an app display name and images. Workspace admins use **Settings → Workspace → App branding** when the organization permits overrides. The same workspace controls are available in Appearance.

The organization default applies immediately to inheriting workspaces. An explicit workspace profile owns a complete snapshot, including copies of inherited image bytes. Changing or resetting an organization logo cannot break an existing workspace override. Disabling overrides removes their content; re-enabling the policy does not restore old branding.

Each profile supports a display name, square logo, optional dark logo, optional horizontal wordmark, and optional dark wordmark. A wordmark replaces the logo-and-text pair in expanded navigation; compact navigation uses the square logo. Missing dark variants use their default image. Failed artwork falls back to another available mark and finally Converge with the display name. Uploaded colors and proportions remain intact.

Names contain 1–60 characters without control characters. Each image accepts PNG, JPEG or WebP up to 1 MiB and 2048 pixels per side. Square logos require equal dimensions. The server fully decodes and normalizes images to PNG, strips embedded metadata, and rejects output larger than 1 MiB. Dark variants require their corresponding default image.

## Runtime boundary

The web app applies branding to navigation, contextual logos/loading screens, document titles and the browser favicon. Native workspace headers and contextual loading screens resolve the same profile and appearance. Native images receive credentials only for same-origin branding endpoints and stay out of the disk cache.

Sign-in and other public entry screens, public profiles, emails, installed PWA/native icons, store listings, documentation, provider authorization, billing/operator identity and technical identifiers retain SocialFlow.ai or their existing identity. Branding is not a global string replacement. Custom domains and public entry branding require a separate product boundary.

Web runtime identity is memory-only and bound to the current user and workspace. Workspace changes, sign-out and public routes must never retain another workspace's identity. Missing branding resolves to SocialFlow.ai. Theme selection and scheme changes do not mutate branding.

## Persistence and authorization

`apps/server/internal/services/appbranding` owns validation, resolution, writes, image access and storage cleanup. `app_branding_profiles` stores one organization default and optional workspace snapshots. Organization policy changes and workspace writes take the shared organization fence and recheck authority inside it. Writes compare the expected profile revision; reset profiles retain revision tombstones so a stale editor cannot overwrite a reset.

Images are saved through `BlobStorage`. An image ID may only retain a current profile image or copy a current organization image into a workspace snapshot. Reads authorize workspace access and current profile reachability; organization-default previews require organization or workspace administration. Responses are private and non-cacheable. Failed writes clean uncommitted objects; replacement, reset, policy removal, workspace deletion, organization deletion and account closure enqueue durable storage cleanup.

Huma owns `/app-branding/{workspace_id}` and its organization/workspace writes and image reads. Generated OpenAPI and `@openpost/api-contract` own the transport schema. `@openpost/query-catalog` owns the shared read key and policy. Organization mutations invalidate the branding reads of every loaded workspace in that organization. Desktop web owns editing; native consumes the effective profile.

## Verify changes

HTTP tests in `apps/server/internal/api/handlers/app_branding_test.go` cover default resolution, organization/workspace permissions, inheritance/reset, revision conflicts, locked overrides, image normalization/access and independent image copies. Migration checks must pass from an empty database. Browser verification covers actual save, reload, workspace inheritance/override, theme changes, failed images, public-entry reset and responsive previews in both schemes. Native verification uses a simulator or device, including a long name and both schemes.
