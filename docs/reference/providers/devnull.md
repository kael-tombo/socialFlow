---
description: Connect dev/null with an SSH key and publish signed commits, replies, and image posts.
---

# dev/null

This page covers the dev/null integration boundary for operators and users.

SocialFlow.ai publishes to [dev/null](https://dev-null.sh) by signing each commit with the SSH private key on your GitHub account, exactly like the official `commit` shell script. There is no OAuth app, callback URL, or server-side provider application to configure.

## Connection

Each person connects with their own dev/null username and private key. SocialFlow.ai stores the key encrypted and uses it only to sign commit payloads.

1. Add the public half of an Ed25519 or RSA key to the GitHub account you commit with.
2. Sign in at `dev-null.sh` so it picks up the key.
3. In SocialFlow.ai, connect dev/null from **Social accounts** with the username and the full private key.
4. Publish a test commit and open the resulting link from the post details.

Posts are attributed to the GitHub account that holds the key. Use a dedicated key without a passphrase.

## Implemented formats

SocialFlow.ai supports text commits, replies, and image posts.

- Body text is 3 to 4000 characters. The adapter normalizes runs of blank lines before sending.
- Up to 3 JPEG, PNG, or WebP images, up to 10 MiB each and 6000 pixels on either side.
- The first link in a commit unfurls automatically on dev/null.
- Thread replies publish as ordinary replies through the shared reply flow.

Polls, quotes, and image descriptions are not supported because the dev/null CLI contract carries only body text, an optional reply reference, and images.

## Limits and approval

- dev/null allows 10 commits a minute.
- New accounts need approval before commits become public.
- Pending commits are visible only to the account that created them until approval.

SocialFlow.ai signs locally with the stored key and does not use a shared dev/null application. A failed signature usually means the key on the account no longer matches the key used to sign.

## No Hosted certification claim

The integration is implemented in the repository, but it has no current SocialFlow.ai Hosted service certification claim. Self-hosted operators follow the [dev/null integration guide](https://openpo.st/docs/self-hosting/integrations/devnull) and do not need to enable any provider application.

Use the [Provider Readiness and Launch Gate](../operations/provider-launch-matrix.md) before changing any public availability claim.
