# SakhiCredit demo capability report

This report covers the recorded standalone SakhiCredit build and the observed
Railway production environment. The [delivery checklist](DEMO-TODO.md) tracks
release and hosted acceptance.

| Capability | Status | Evidence and boundary |
|---|---|---|
| Application, request progress and approved fields | Working locally and on Railway | The [standalone run](infra/demo/README.md) starts a separate SakhiCredit service. The browser retrieves the approved fields and can reopen its stored result. The product makes no lending decision. |
| OAuth client authentication | Working locally and on hosted Hydra | Official Hydra issues and introspects tokens in integration tests. Railway's split services are active and public discovery passes. Both hosted demo clients are registered and the live SakhiCredit journey used them. [Hydra deployment](infra/railway/README.md#hydra-deployment) |
| Person authentication | Mocked identity on official eSignet | Official eSignet and its UI complete the OIDC login. Its identity backend and OTP are fictional fixtures. The local single-origin test exercises both the browser and token exchange. The hosted eSignet issuer is healthy. Hosted Chrome completed the OIDC journey using the seeded fictional identities. Government credentials have not been integrated. [eSignet setup](infra/esignet/README.md) |
| Registry ownership and fields | Mocked LokOS source | The separate fictional LokOS service maps approved demo subjects and returns synthetic membership and savings fields. The SDK-backed adapter verifies binding and projects approved fields. Actual LokOS onboarding requires the native provider implementation and approved credentials. |
| Consent and withdrawal | Working locally | The exact notice is presented in English and needs explicit confirmation. The downloaded receipt binds the exact rendered notice and its fingerprint to the web channel, English template version and authenticated confirmation. Approval, rejection and withdrawal are covered. Changing the scope requires cancelling and creating a fresh request. |
| Scoped retrieval and replay denial | Working locally | The gateway and SDK verify policy, subject, consent, grant, mTLS peer and single-use admission. Replay, dependency failure, restart and four-slot capacity checks passed. [Acceptance evidence](DEMO-TODO.md) |
| Evidence custody and DeDi | Working locally | Signed evidence is retained before release. The actual DeDi node accepts a commitment and provides an offline-verifiable inclusion proof. Batch anchoring can follow release. It does not prove independent witness consistency, payload truth or downstream deletion. The hosted DeDi node is healthy. The production registry accepted a synthetic signed commitment with a verified proof and idempotent retry. Browser-generated commitments were verified in the local suite. |
| Hosted app deployment | Working on Railway | SakhiCredit, the gateway and adapter metadata passed readiness. Hosted Chrome verified approval, membership-only disclosure and refusal. The fictional source and DeDi use private networking. [Railway topology](infra/railway/README.md) |
| Adapter distribution | Public Go module | LokOS pins Go SDK v0.1.0 and builds independently. The tag downloads anonymously through the public Go proxy with checksum verification. The reusable conformance checks cover provider, discovery, authority and protected retrieval behavior. [SDK instructions](sdk/go/adapter/README.md) |
| Extended consent and rights | Working locally | The notice binds purpose and processing versions, field explanations, and recipient responsibilities. The demo covers single-use access, recipient-reported legal hold, fresh sign-in to consent history, receipt download, and an acknowledged correction case. Operator tests cover registry and recipient outcomes. Independent verification and historical source windows are unsupported. |
| Authority and channels | Unsupported in demo | The demo uses first-party English web confirmation. Delegation, alternate presentation channels, additional identity provider profiles and voice confirmation are unsupported. Voice and AI assistants belong in product integrations. |
| Legal status and retention proof | Unsupported in demo | The demo makes no legal registration, DPDP certification or DEPA conformance claim. A receipt or commitment does not prove that a recipient deleted its copy. |

## Acceptance evidence

- The combined local run passed seven Chrome scenarios against official
  eSignet, Hydra and DeDi. It verified 12 retained disclosure commitments.
  [Redacted local acceptance](DEMO-TODO.md#combined-local-acceptance-on-533a7be)
- GitHub Actions passed the independent module, PostgreSQL, DeDi and
  Hydra/browser jobs. Hosted CI's browser job uses the fast fictional IdP.
  [Go and browser CI](https://github.com/theflywheel/GRAMStack/actions/runs/41780644767)
- The hosted documentation, Hydra and eSignet endpoints passed the public probe.
  All four probed eSignet management routes returned 404. The private DeDi
  deployment passed its Railway health check. Hosted Chrome passed approval,
  membership-only disclosure and refusal. Failure injection and offline
  verification of browser-generated proofs were exercised in the local suite.
  [Railway public check](infra/railway/README.md#public-acceptance-check) · [Live implementation profile](https://docs.gramstack.try-dough.com/specifications/post-disclosure)

## Recording sequence

1. Launch fresh local state with the documented standalone command. Run the
   recorder with a new output path. The driver omits entry of the fictional
   credentials.
2. Show the requested fields, the eSignet handoff, registry verification, the
   exact consent notice and explicit approval. Show the two approved values
   in SakhiCredit, then show the protected receipt with the fictional
   legal-hold report and its verification boundary.
3. Create a separate application and reject its notice. Show the rejected
   receipt and SakhiCredit's declined state with retrieval unavailable.
   Sign in from a fresh browser context to show both history records. Submit
   a correction case, then refresh to show the registry acknowledgement.
4. The rehearsal shows the live read-only verifier's aggregate DeDi result,
   then displays this matrix from the checked-in report. Update the report
   against final acceptance evidence before making the final recording.
   Verify playback before sharing the file.
