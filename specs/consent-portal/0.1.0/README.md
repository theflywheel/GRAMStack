# First-party consent portal profile 0.1.0

This profile defines the server-rendered WEB/English interaction and read-only consent record alongside immutable core 0.2.0. It does not change the core Decision JSON or add a human-decision bypass API. Responses identify `Gramstack-Consent-Portal-Version: 0.1.0`. The browser obtains the exact form and its presentation binding from this portal; arbitrary clients cannot submit core Decision JSON to these form routes.

## Routes and synchronous boundaries

| Method and route | Input and authentication | Result |
|---|---|---|
| GET `/interactions/{id}/consent` | Request-bound authenticated session; optional single `lang=en` | Exact immutable notice, session-bound presentation and forms; unsupported language is 400 |
| POST `/interactions/{id}/decision` | URL-encoded form, exact Origin, session and CSRF | Atomic decision plus presentation link, consent status and outbox; 303 to record |
| GET `/interactions/{id}/receipt` | Same request-bound session | Read-only human record; `?format=json` downloads JSON |
| POST `/consents/{id}/withdrawal` | Owner session, exact Origin, URL-encoded `csrf` only | Atomic withdrawal plus outbox; 303 to record |
| GET `/assets/consent.css` | Public static asset | Styles only; no sensitive state |

Decision forms carry exactly `csrf`, `decision`, `request_digest`, `notice_digest`, `notice_version`, `language`, `presentation_id`, and optional `confirmation`. Approval requires `confirmation=yes` and an initially unchecked required checkbox. Rejection requires no approval checkbox. The recorded `explicit_confirmation` denotes the approval checkbox, not a claim that a rejection was implicit. Unknown/duplicate form fields, unsupported actions (including partial refusal), substituted presentations/digests, cross-origin and session/principal substitution are rejected. The browser cannot select identity or a source mapping. A changed scope needs cancellation and an independent new request.

A presentation MUST match the consent, authenticated principal, session digest, exact notice, revision and language and remain unexpired at decision commit. The immutable presentation records a server-rendered response; it is not proof of reading, understanding or human identity by itself. Its private principal and session digests are not exported. Decision evidence, authority change and metadata outbox MUST commit together or all roll back. Identical action retries return existing status; conflicting actions fail. Expired presentation retries do not create fresh authority.

Approval is synchronous through `APPROVAL_RECORDED`. Signing/activation are separate trusted asynchronous work; GET, redirects and receipt export MUST NOT sign, activate or renew consent. The record MUST report absence of a signed artefact honestly. Withdrawal blocks future admissions and preserves the original approval; a reserved one-use attempt remains consumed. `admission_cutoff_passed` is not a delivery confirmation. Current policy, validity and authoritative status still govern enforcement even if an older record reports ACTIVE.

## Custody and receipt semantics

`consent_presentations` retains the notice binding, server timing and protected session/principal correlation. `consent_decision_presentations` links the immutable decision to that evidence. Existing consent SQL retains exact notice text, recipient terms, decision and status; immutable artefacts are stored separately. No upstream identity, subject locator, source mapping or business payload belongs in the exported record or outbox.

[schemas.json](schemas.json) describes `gramstack.consent-record.v1`; [examples.json](examples.json) provides fictional positive/negative fixtures. `generated_at` describes a repeatable-read database observation. `request_status` and consent state are independent workflow observations and can differ while reconciliation is pending. A JSON download is a server-generated record, not itself a signed receipt; only `signed_artefact`, when present and verified, carries the consent authority's signature. Historical rows without a presentation link must not be represented as having presentation proof.

HTML and downloads use no-store, nosniff, same-origin referrer policy, CSP with self-only form submission and no third-party assets. Same-origin referrer policy preserves browser Origin on authenticated form POSTs. Export requires authentication; opaque URLs alone confer no authority. This profile's current interaction session expires with its request/authentication bound. Later protected history/recovery is a separate capability and MUST NOT be claimed from this short-lived session.

English template `gramstack.en.v1` and WEB are the supported presentation profile. Other languages, voice playback, assistance, guardians/delegates and accessibility certification are not implied. No product-specific voice assistant is part of this platform contract.
