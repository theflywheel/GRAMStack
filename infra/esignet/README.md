# Local eSignet demo stack

Official eSignet 2.0.0 service and UI, official mock identity system 0.14.0, PostgreSQL and Redis. Images are pinned by digest. All published ports bind to `127.0.0.1`; this configuration uses public demo passwords and software keystores. It is for isolated local development. Railway requires separate secrets, routing and ingress restrictions before deployment.

```sh
open -a Docker
docker compose -p gramstack-esignet -f infra/esignet/compose.yaml up -d
python3 infra/esignet/check.py
```

The UI is at http://127.0.0.1:19300 and the issuer is http://127.0.0.1:19080. First startup may take a minute. Client onboarding and fictional identity seeding are separate integration steps; a healthy discovery document does not demonstrate gateway login. The mock source is not government authentication.

Postgres, Redis and **both** service keystores use named volumes. Recreate containers without deleting those volumes. Database key references and their keystore must be backed up/restored together. `down` retains volumes; `down -v` destroys this demo's state. The init SQL runs only on a fresh database volume. It creates dedicated demo databases and must never run against an existing shared database.

If Docker Desktop's credential helper waits on a locked macOS keychain, public image pulls can use an isolated Docker config with empty `auths`. Do not change the user's existing Docker credentials. Use that same config for all commands in the session.

## Provenance

SQL and nginx configuration originate from [MOSIP eSignet v2.0.0](https://github.com/mosip/esignet/tree/db528423dac7898ad546cb7b2f5f867a949676a7/docker-compose), commit `db528423dac7898ad546cb7b2f5f867a949676a7`, under MPL-2.0 (included). The upstream personal-looking example identity was omitted from SQL. Compose adds Redis, persistent key files, dependency health checks, loopback ports and immutable image references. Upstream documentation: https://docs.esignet.io/.

Provider integration must support eSignet's advertised `private_key_jwt` token authentication and `PS256` ID tokens. Keep this in the auth-provider adapter/profile; Hydra and request/session enforcement remain shared platform behavior. See D03a in the demo checklist for the login acceptance gate.
