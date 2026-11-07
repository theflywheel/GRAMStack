---
title: Deployment and configuration
---

`infra/railway/railway.ts` describes the multi-service Railway project. Each service has its own build, networking, health check and deployment settings. Code services use their own Dockerfiles; upstream services such as eSignet use dedicated image/configuration wrappers.

| Public surfaces | Private surfaces |
|---|---|
| Documentation, SakhiCredit, gateway browser/API endpoints and Hydra public protocol | Hydra admin, management endpoints, protected data transport, adapter RPCs and service databases |
| Service-hosted Swagger and adapter capability metadata | Runtime secrets, source credentials, consent signing keys and service mTLS identities |

The docs application is maintained under `docs/`. Its build context includes the canonical sibling `specs/` directory. Swagger remains on the actual API services.

## Deploy a committed docs revision

```sh
stage=$(./infra/railway/stage.sh docs HEAD)
railway up "$stage" --path-as-root --service docs --environment production --detach
rm -rf "$stage"
```

The staging script selects an allowlisted committed source tree. Review and commit changes before deploying. Runtime secrets are maintained in Railway rather than committed source or image layers. Preserve stable encryption and identity keys across restarts.

Database migrations are explicit deployment operations. Runtime readiness checks installed checksums; application startup does not silently apply schema changes. Use dedicated service roles and Railway references where supported.

[Infrastructure configuration and onboarding](https://github.com/theflywheel/GRAMStack/tree/main/infra/railway) · [Docs authoring](https://github.com/theflywheel/GRAMStack/blob/main/docs/README.md)
