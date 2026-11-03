# CI speed and correctness

Measure hosted job/step timings after each substantial demo phase. Optimise elapsed feedback time and total runner work; keep failure output actionable. A passing cached unit test is not a substitute for fresh integration tests against live dependencies.

## Checks preserved

| Check | Coverage |
|---|---|
| Go verifier | Independent GOWORK=off module race tests and full vet, canonical FSM/spec comparison, executable builds, liveness/unavailable-readiness/protected-route checks and graceful shutdown |
| Parallel-runner self-check | Injected test/vet failures must make the parent fail before builds; collect both active workers |
| PostgreSQL | Fresh temporary database, migrations, concurrency, idempotency/restart/rollback, consent/enforcement and encrypted authentication state |
| Hydra | Actual pinned/checksum-verified Hydra, real token issuance/introspection/revocation, consumer-only gateway process, then federation-enabled gateway process and separate fictional IdP |
| DeDi | Actual pinned node, signed publication, trust-pinned inclusion and gateway CLI |
| Docs | Clean npm install, contract/fixture validation, Quartz build, exported content/links/JSON and server checks |
| Infrastructure | Independent tooling install and infrastructure configuration verification |

Integration commands retain `-count=1`, fresh isolated stores and live processes. No integration result cache is introduced. Failed workers remain fatal; no continue-on-error, silent retries, weaker race/signature checks or reduced negative cases.

## Current optimisations

- Independent verifier work runs two workers at a time, with separate readable logs and every exit status collected. Builds also run in a bounded pair. Independent modules still resolve without implicit workspace dependencies.
- Hydra/SQL startup and the gateway build are shared across consumer-only and federated configurations. The runner stops the first gateway, starts the second with authentication enabled and still tests both configurations; this removes a duplicate hosted job, not a tested configuration.
- The same built gateway executable performs migrations and process checks; no extra go-run link step.
- Dependency caches include all manifests/checksums relevant to each job. Keep lockfiles and exact tool versions. Do not cache live databases, protocol state, credentials or test outcomes.
- Cancel superseded verification runs only within the same workflow/ref. Go, docs and infrastructure remain separate groups; deployments are not cancelled by these settings.
- Existing path filters avoid unrelated workflows; Go workspace checksum and runner-test changes also trigger verification. Ten-minute job limits bound hung work without tightening valid test deadlines.

Hydra download was approximately one second in the baseline; a separate archive cache is not justified by that measurement. Avoid source-wide build-cache invalidation by default: it creates a new large cache on every source change. Review further cache work when timings show a benefit.

## Measurements

Baseline hosted warm run: [7c6b17a](https://github.com/theflywheel/GRAMStack/actions/runs/41612002970).

| Job | Elapsed seconds |
|---|---:|
| Go verifier | 46 |
| PostgreSQL | 27 |
| DeDi | 29 |
| Hydra consumer-only | 30 |
| Hydra federation | 29 |
| Total job elapsed (sum; not billed minutes) | 161 |

The verifier execution step was 22 seconds; Go setup/cache restore was 10–14 seconds per job. The docs baseline [9d8f991](https://github.com/theflywheel/GRAMStack/actions/runs/41607163502) was 22 seconds including setup/install/build.

On the local warm cache, the unchanged full verification command measured 6.54 seconds before parallelisation and 3.78 seconds after. These are single local observations, not a hosted-CI speed guarantee. The [warm hosted repeat](https://github.com/theflywheel/GRAMStack/actions/runs/41613574810/attempts/2) passed every check: overall elapsed 33 seconds and summed job elapsed 105 seconds (verifier 21, PostgreSQL 24, DeDi 28, combined Hydra 32). Relative to the baseline, those single-run observations are about 28% less elapsed time and 35% less summed job work, not a statistical benchmark or billing estimate.

The first run with revised dependency keys also passed, but took 134 seconds overall / 453 summed job seconds while recompiling cold caches. Keep this cost visible when changing dependencies; do not compare a cold run with a warm baseline. Exact dependency caches are retained. A proposed broader toolchain-only fallback was rejected by automatic approval review due to the expanded cache-poisoning trust boundary and was not implemented.

## Ongoing acceptance

For each new phase, inspect the critical job/step and runner-work totals, preserve a coverage map, and investigate meaningful regressions. Keep the standard developer commands usable; do not require hosted CI to catch failures that a fast local command can expose.

## Independent auth adapter

`adapters/auth/oidc` is checked independently with `GOWORK=off`, race tests and vet in the existing verify job; its dependency files join that job’s exact cache key. The fault-injection runner also checks auth-adapter test/vet failures stop the build gate. No extra CI job or Docker image pull was added to the normal push path. The actual eSignet local integration test is opt-in (`GRAMSTACK_TEST_ESIGNET=1`) and was run against the pinned stack; it is distinct from the fast protocol tests and the pending gateway browser journey.

Gateway integration now resolves the private, pre-release auth module through a temporary module file in every independent runner (`scripts/gateway-module.sh`). Exact cache keys include the auth module's dependency files in each affected job. Public checksum verification is unchanged; production release/distribution remains D13. Normal push CI retains the fast mock-provider full-federation test. The real eSignet federation variant is run locally with `GRAMSTACK_TEST_ESIGNET=1` against the pinned Docker stack, avoiding heavy image pulls on every push.
