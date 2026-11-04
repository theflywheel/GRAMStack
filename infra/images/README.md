# Go service images

Build each service from the repository root using its own Dockerfile:

| Role | Dockerfile | HTTP listener | Protected listener |
|---|---|---|---|
| Gateway | `services/gateway/Dockerfile` | `PORT` (8080) | Reviewed authorization configuration |
| LokOS adapter | `adapters/lokos/Dockerfile` | `PORT` (8080), anonymous metadata and health | `ADAPTER_TLS_PORT` (8443), service mTLS |
| Fictional LokOS source | `mocks/lokos/Dockerfile` | None | `SOURCE_TLS_PORT` (8444), adapter mTLS |
| SakhiCredit | `apps/sakhicredit/Dockerfile` | `PORT` (8080), product UI/API | Outbound consumer mTLS |

For example: `docker build -f services/gateway/Dockerfile -t gramstack-gateway .`.
The pinned Go 1.26.8 builder resolves the current monorepo workspace. The final pinned Debian image contains the selected service, a standard-library file entrypoint and CA trust roots. It runs as UID/GID 10001 with no compiler, Node dependency or embedded deployment secret. SDK publication and independent versioned dependency verification remain a separate release gate.

## Railway file configuration

Supply `GRAMSTACK_FILES_JSON` as a **secret** environment variable: a JSON object mapping flat filenames to standard base64 file contents. The entrypoint materializes only these files under `/run/gramstack`, a private 0700 directory; files are 0600. Set the service's existing file environment variables to the corresponding absolute paths, for example `GRAMSTACK_CONFIG_FILE=/run/gramstack/gateway.json`. The bundle is removed from the service process environment before execution. Other service environment variables retain their existing meaning.

The bundle is bounded to 32 files, 2 MiB encoded JSON and 1 MiB total decoded content. Invalid names, duplicate keys, invalid base64, symlinks, unsafe permissions and conflicting existing contents fail startup without exposing the contents in errors. An identical bundle supports process restart. Rotate configuration or keys by deploying a fresh container; a conflicting existing file is never silently overwritten. This is a deployment convenience, not a secret-management API, certificate issuer or authority to change trusted keys.

Do not put this secret JSON, actual certificates, private keys or database credentials in Git, image build arguments or CLI arguments. CLI examples intentionally contain no secret bundle. Mount approved files directly instead when the deployment supports secret volumes.

Run migrations explicitly using the same image and approved environment:

```sh
docker run --rm --env-file /private/operator/gateway.env gramstack-gateway /app/service migrate
```

The env file must be private and operator-provided. Startup/readiness does not apply DDL. Publish the public HTTP listeners through HTTPS ingress; protected listeners stay private with their existing TLS/mTLS checks. The fictional source is a separate demo-only image and refuses startup without explicit development/demo configuration.

Run `./scripts/test-images.sh` to build all four real images and check non-root execution, secret file permissions, removal of the bundle from the child environment, unconfigured readiness, mock configuration denial and graceful service shutdown. Use an isolated `DOCKER_CONFIG` if local credential helpers are unavailable. This image check alone does not prove a configured deployment, fresh stack or end-to-end acceptance.
