# SakhiCredit demo capability report

This is the local rehearsal report. It records what has been verified and what
remains open for the final demo. The [delivery checklist](DEMO-TODO.md) tracks
the acceptance gates.

| Capability | Status | Evidence and boundary |
|---|---|---|
| Application, request progress and approved fields | Working locally | The [standalone run](infra/demo/README.md) starts a separate SakhiCredit service. The browser retrieves the approved fields and can reopen its stored result. The product makes no lending decision. |
| OAuth client authentication | Working locally and partially hosted | Official Hydra issues and introspects tokens in integration tests. Railway's split services are active and public discovery passes. Hosted demo client registration is pending. [Hydra deployment](infra/railway/README.md#hydra-deployment) |
| Person authentication | Real protocol with mocked identity | Official eSignet and its UI complete the OIDC login. Its identity backend and OTP are fictional fixtures. The local single-origin test exercises both the browser and token exchange. Government credentials and hosted eSignet activation are pending. [eSignet setup](infra/esignet/README.md) |
| Registry ownership and fields | Mocked source, working enforcement | The separate fictional LokOS service maps approved demo subjects and returns synthetic membership and savings fields. The SDK-backed adapter verifies binding and projects approved fields. Actual LokOS onboarding requires the native provider implementation and approved credentials. |
| Consent and withdrawal | Working locally | The exact notice is presented in English and needs explicit confirmation. Approval, rejection, withdrawal and the interaction receipt have tests. Changing the scope requires cancelling and creating a fresh request. |
| Scoped retrieval and replay denial | Working locally | The gateway and SDK verify policy, subject, consent, grant, mTLS peer and single-use admission. The integration suite tests duplicate attempts and source and evidence failure. [Acceptance evidence](DEMO-TODO.md) |
| Evidence custody and DeDi | Working locally | Signed evidence is retained before release. The actual DeDi node accepts a commitment and provides an offline-verifiable inclusion proof. Batch anchoring can follow release. It does not prove independent witness consistency, payload truth or downstream deletion. Hosted DeDi activation is pending. |
| Hosted app deployment | Prepared | Railway has PostgreSQL, Hydra and the application and identity service topology. The complete hosted journey still needs authorized runtime settings, onboarding and acceptance. [Railway topology](infra/railway/README.md) |
| Adapter distribution | Workspace verified | LokOS uses the separate Go SDK module. A published module release and external conformance remain D13. [SDK instructions](sdk/go/adapter/README.md) |
| Extended consent and rights | Pending | D14 tracks versioned purpose/processing metadata, broader channel and language evidence, protected cross-request history, downstream handling reports and rights/grievance cases. The current interaction receipt is narrower. |
| Authority and channels | Limited | The demo uses first-party browser confirmation. Delegated authority, alternate presentation channels and additional identity provider profiles have no demo acceptance evidence. Voice and AI assistants belong in product integrations. |
| Legal status and retention proof | Outside demonstrated scope | The demo makes no legal registration, DPDP certification or DEPA conformance claim. A receipt or commitment does not prove that a recipient deleted its copy. |

## Recording sequence

1. Launch fresh local state with the documented standalone command. Run the
   recorder with a new output path. The driver omits entry of the fictional
   credentials.
2. Show the requested fields, the eSignet handoff, registry verification, the
   exact consent notice and explicit approval. Show the two approved values
   in SakhiCredit.
3. Create a separate application and reject its notice. Show the rejected
   receipt and SakhiCredit's declined state with retrieval unavailable.
4. The rehearsal shows the live read-only verifier's aggregate DeDi result.
   For the final recording, present this matrix updated against the final
   acceptance evidence. Verify playback before sharing the file.
