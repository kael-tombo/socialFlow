# Image generation

`services/imagegeneration` owns Image Editor generation, selected-image edits and masked fills. The authenticated Huma API admits requests with a user- and Workspace-scoped idempotency key. Saved jobs perform inference. `ai.ImageProvider` owns fal I/O, and the normal Media pipeline owns generated files.

The default is GPT Image 2.5 Flare; the shared server catalogue also exposes Sunburst. Sources and masks are normalized to PNG. Source reads cross `BlobStorage`, accept only ready library images in the same Workspace, and are bounded to 32 MiB and 16 million pixels. Fill output is clipped server-side to the submitted mask, regardless of the model's changes outside it.

## Paid dispatch and recovery

Admission stores the immutable request, authority and job in one transaction. A lease fences each worker advancement. The worker revalidates edit authority before dispatch and before importing its result. Dispatch records an `editor_ai_calls` row with provider `fal` and the image operation.

Submission is sent exactly once. The maintained fal Go SDK retries transport errors, including POST, so the adapter uses a single HTTP POST for submission and the SDK for receipt status and result reads. A saved `submitting` request without a receipt becomes `unknown` after interruption. It is never automatically purchased again. Browser retries reuse their idempotency key after ambiguous network or server failures.

Accepted receipts survive worker restarts. Download and import failures retry for up to 24 hours. Each run derives a stable internal Media UUID, and new Media insertion and upload accounting commit together. A failed recipe or run update therefore reuses the same attachment. Each attachment retains an immutable recipe with prompt, settings, source, target and provider receipt. Raw masks remain in the saved request, not the Media recipe.

Terminal queue failures update the visible run in the queue transaction and preserve paid billing recovery. Completed images remain completed while cost reconciliation continues. Costs come from fal billing events matching both endpoint and request ID, rounded from nano-USD to the ledger's micro-USD. Missing events stay pending for up to 72 hours, then become unknown. No token usage or price is invented.

## Applying results

The browser owns application to its open document. It hashes the original page or image layer, including page dimensions, and checks that fingerprint before changing anything. Edits preserve the original layer ID and authored placement, crop and effects. Fill adds a transparent overlay. Each action enters the editor's existing atomic undo history. The user can explicitly add a stale image edit as a new layer.

Query keys include actor, Workspace and project. The panel resumes saved history after navigation and keeps refreshing while inference or billing is pending. Anonymous editing remains local; signed-in local projects use the existing Save to SocialFlow.ai transfer before accessing generation.

## Configuration and migration

Set `FAL_KEY` or `FAL_KEY_FILE` on both web and worker roles. A key needs inference and billing-event access. Migration 155 adds the image run table and provider/operation attribution to the existing usage ledger. Existing calls retain `openrouter` and `assistant` defaults. Hosted must migrate before starting web and worker. Rollback leaves the additive schema in place; keep the new worker available to finish accepted paid requests before removing the configuration.

Provider contracts were checked against the official [Flare generation](https://fal.ai/models/openai/gpt-image-2.5/flare/text-to-image/api), [editing](https://fal.ai/models/openai/gpt-image-2.5/flare/edit/api), [queue](https://fal.ai/docs/documentation/model-apis/inference/queue) and [billing](https://fal.ai/docs/platform-apis/v1/models/billing-events) documentation on 2026-10-11. The source/mask/result workflow also follows the explicit inputs and output review seen in [ComfyUI's OpenAI image nodes](https://docs.comfy.org/tutorials/api-nodes/openai/gpt-image-1). No reference UI code was copied.
