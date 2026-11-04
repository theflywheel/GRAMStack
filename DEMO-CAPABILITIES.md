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
| Consent and withdrawal | Working locally | The exact notice is presented in English and needs explicit confirmation. The downloaded receipt binds the exact rendered notice and its fingerprint to the web channel, English template version and authenticated confirmation. Approval, rejection and withdrawal are covered. Changing the scope requires cancelling and creating a fresh request. |
| Scoped retrieval and replay denial | Working locally | The gateway and SDK verify policy, subject, consent, grant, mTLS peer and single-use admission. The integration suite tests duplicate attempts and source and evidence failure. [Acceptance evidence](DEMO-TODO.md) |
| Evidence custody and DeDi | Working locally | Signed evidence is retained before release. The actual DeDi node accepts a commitment and provides an offline-verifiable inclusion proof. Batch anchoring can follow release. It does not prove independent witness consistency, payload truth or downstream deletion. Hosted DeDi activation is pending. |
| Hosted app deployment | Prepared | Railway has PostgreSQL, Hydra and the application and identity service topology. The complete hosted journey still needs authorized runtime settings, onboarding and acceptance. [Railway topology](infra/railway/README.md) |
| Adapter distribution | Tagged private module | LokOS pins Go SDK v0.1.0 and builds independently with repository access. The reusable conformance checks cover provider, discovery, authority and protected retrieval behavior. Public distribution remains open. [SDK instructions](sdk/go/adapter/README.md) |
| Extended consent and rights | Pending | The notice binds versioned purpose and processing terms, including field explanations, the responsible organisation and declared processors. The demo supports bounded single-use access to a current summary. The private gateway accepts authenticated handling reports, and the protected consent receipt displays them as recipient assertions. Fresh eSignet sign-in recovers cross-request history and downloads a protected receipt. Live integration verifies owner case intake and private registry and recipient status updates. Independent verification and historical source windows remain outside the demo. |
| Authority and channels | Limited | The demo uses first-party browser confirmation. Delegated authority, alternate presentation channels and additional identity provider profiles have no demo acceptance evidence. Voice and AI assistants belong in product integrations. |
| Legal status and retention proof | Outside demonstrated scope | The demo makes no legal registration, DPDP certification or DEPA conformance claim. A receipt or commitment does not prove that a recipient deleted its copy. |

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
   Sign in from a fresh browser context to show both history records.
4. The rehearsal shows the live read-only verifier's aggregate DeDi result,
   then displays this matrix from the checked-in report. Update the report
   against final acceptance evidence before making the final recording.
   Verify playback before sharing the file.
