---
title: Integration quickstart
---

## Enroll the application

Ask the platform operator to register a consumer client, approved registry/resource/fields, purpose and recipient terms. Protected retrieval also requires a registered mTLS certificate and connectivity to the private data endpoint. Install an [application SDK](/developers/sdks) on your backend.

For standalone application-user sign-in, enroll a separate OpenID-only client and exact callback. Configure the SDK's shared encrypted one-use login store. Verify the returned identity before creating your product session. Sign-in alone does not authorize registry access.

## Request, approve and retrieve

1. Configure the public gateway, OAuth token endpoint and pinned private data origin. Keep consumer credentials and mTLS keys on the backend.
2. Create a request with the exact fields and a stable idempotency key.
3. Show the returned first-party interaction URL to the person. They authenticate, verify registry ownership and approve or decline the exact notice.
4. Poll metadata until consent is active or the request reaches a terminal state.
5. Persist your retrieval attempt, issue a grant with a stable key, and retrieve once through the SDK.
6. Retain the receipt reference and handle the received copy under the approved terms.

A lost response may mean uncertain delivery. Do not automatically retry retrieval or manufacture new authority. Inspect status and the evidence trail. Changing scope requires cancellation and a new explicit request.

[Choose an SDK](/developers/sdks) · [Inspect service APIs](/developers/api-reference) · [Try the reference product](/demos/sakhicredit)
