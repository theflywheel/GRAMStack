# GRAMStack on Kubernetes

This Helm chart deploys the Go gateway and LokOS adapter. The fictional LokOS
source is optional and intended for isolated tests. Platform clients deploy
separately and use the gateway's public API and private mTLS data endpoint.

## Prerequisites

- A Kubernetes namespace, Helm, and a CNI that enforces NetworkPolicy if you
  rely on the chart's pod-level ingress rules.
- PostgreSQL with separate gateway and adapter databases and roles. They can
  share a PostgreSQL server. Provision the databases before running Helm.
- A configured Hydra instance, approved OIDC provider such as eSignet, and
  registry source. Provide a DeDi node and its publisher trust when enabling
  anchoring. Configure these dependencies separately and keep their management
  endpoints private.
- Approved runtime configuration, certificates, signing keys and client
  registrations. Use your own HTTPS origins and verified service identities.

The chart references existing Secrets in the release namespace. It does not
generate credentials, register OAuth clients or install registry policies.

## Build or choose images

From the repository root:

```sh
docker buildx build --load -f services/gateway/Dockerfile -t gramstack-gateway:local .
docker buildx build --load -f adapters/lokos/Dockerfile -t gramstack-lokos-adapter:local .
# Optional fictional source
docker buildx build --load -f mocks/lokos/Dockerfile -t gramstack-mock-lokos:local .
```

On kind, load the selected tags with `kind load docker-image`. For a remote
cluster, publish the images to your registry and set `gateway.image`,
`lokosAdapter.image`, and optionally `mockLokos.image` to immutable references.
Configure `imagePullSecrets` as a list of Kubernetes Secret references when
your registry requires authentication.

For public Docker Hub images, include
`-f infra/helm/gramstack/values-dockerhub.yaml` before your deployment-specific
values file. The file pins the 0.1.0 image indexes for Linux AMD64 and ARM64.
Omit the `image` keys in your deployment-specific values to use those pins.

To use the published GHCR images, include `-f infra/helm/gramstack/values-ghcr.yaml`
before your deployment-specific values file. This pins the 0.1.0 image indexes
for Linux AMD64 and ARM64. The optional fictional source still requires
`mockLokos.enabled=true`.

For private GHCR packages, create a registry pull Secret in the release namespace
with an account that has package access and configure it in `imagePullSecrets`.
Public packages can be pulled anonymously. See
[GitHub Container Registry authentication](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#authenticating-to-the-container-registry).

## Configure service Secrets

Prepare separate, private env files and directories using the service's
documented configuration. Each file Secret mounts at `/etc/gramstack`. Point
file settings at that directory. Existing `GRAMSTACK_FILES_JSON` bundles can
instead go in the env Secret and continue using `/run/gramstack`.

| Service | Env Secret keys and configuration |
| --- | --- |
| Gateway | `DATABASE_URL`, `GRAMSTACK_CONFIG_FILE`, and the enabled auth, signing, transport and evidence settings. Start with the [gateway configuration example](../../../services/gateway/config.example.json) for the consumer API, then use the [gateway setup guide](../../../services/gateway/README.md) for consent and protected retrieval. |
| LokOS adapter | `ADAPTER_STATE_DATABASE_URL`, `ADAPTER_STATE_KEY`, `SERVICE_CA_FILE`, `SERVICE_CERT_FILE`, `SERVICE_KEY_FILE`, `LOKOS_SOURCE_URL` and `LOKOS_SOURCE_SERVER_NAME`. See the [adapter setup guide](../../../adapters/lokos/README.md) for retrieval and signing settings. |
| Fictional source | `GRAMSTACK_ENV=demo`, `OIDC_ISSUER`, `OIDC_SUBJECT_BINDINGS_FILE`, `SERVICE_CA_FILE`, `SERVICE_CERT_FILE`, and `SERVICE_KEY_FILE`. Use fictional data and explicitly enable the adapter's demo verifier. |

Keep OAuth client registrations, stable encryption keys, signing material and
per-service trust consistent across restarts. The gateway's private
authorization listener must bind to `:8445` when configured. The chart sets
the adapter listener to `8443` and the mock listener to `8444`. Leave
`BIND_HOST` unset so probes and Services can reach the pod listeners. Set all public
origins, callbacks and internal URLs to the new deployment's endpoints.

For release `gramstack` in namespace `gramstack`:

| Route | Kubernetes Service | Transport |
| --- | --- | --- |
| Gateway browser/API | `gramstack-gateway:8080` | HTTP behind HTTPS ingress |
| Gateway data and custody | `gramstack-gateway-private:8445` | mTLS |
| Adapter metadata | `gramstack-lokos-adapter:8080` | HTTP behind HTTPS ingress if needed |
| Adapter verification and retrieval | `gramstack-lokos-adapter-private:8443` | mTLS |
| Optional fictional source | `gramstack-mock-lokos:8444` | mTLS |

Use the in-cluster DNS name in private URLs and retain the configured TLS
server name and peer identity expected by the application. For example,
`LOKOS_SOURCE_URL=https://gramstack-mock-lokos:8444` can use
`LOKOS_SOURCE_SERVER_NAME=mock-lokos.gramstack.internal` with the matching
certificate. Gateway adapter discovery requires an approved HTTPS metadata
origin when that feature is enabled.

## Install

Create the namespace and Secrets before installation. The file Secret commands
assume the env files use `/etc/gramstack` paths.

```sh
kubectl create namespace gramstack
kubectl -n gramstack create secret generic gramstack-gateway --from-env-file=/private/gateway.env
kubectl -n gramstack create secret generic gramstack-gateway-files --from-file=/private/gateway-files/
kubectl -n gramstack create secret generic gramstack-lokos-adapter --from-env-file=/private/adapter.env
kubectl -n gramstack create secret generic gramstack-lokos-adapter-files --from-file=/private/adapter-files/
```

Example `cluster-values.yaml`:

```yaml
gateway:
  image: registry.example.com/platform/gramstack-gateway:0.1.0
  fileSecret: gramstack-gateway-files
  ingress:
    host: gateway.example.com
    className: nginx
    tlsSecret: gramstack-gateway-tls
lokosAdapter:
  image: registry.example.com/platform/gramstack-lokos-adapter:0.1.0
  fileSecret: gramstack-lokos-adapter-files
```

Use the release image tags you actually built. To enable an ingress, supply a
TLS Secret and an installed ingress controller. Use its HTTPS policy for
plaintext-to-HTTPS redirects. Both mTLS Services are ClusterIP and stay outside
the HTTP ingress.

```sh
helm upgrade --install gramstack ./infra/helm/gramstack \
  --namespace gramstack --values cluster-values.yaml --wait --timeout 8m
kubectl -n gramstack get deployments,pods,services
```

Helm runs the existing gateway and adapter migration commands as Jobs before
install and upgrade. By default each Job uses the corresponding runtime env
Secret. Set `gateway.migrationSecret` or `lokosAdapter.migrationSecret` to use
a separate migration env Secret containing the required settings and DDL
credentials. Give the runtime role access to the schema objects created by
the migration role. The same optional file Secret mounts in the Job. On failure,
inspect the retained `<release>-<component>-migrate` Job logs in the namespace.
Readiness checks validate the installed schemas.

The private listener policies allow the corresponding same-release peers.
Configure `gateway.privatePeers` with explicit Kubernetes
`NetworkPolicyPeer` entries for approved consumers. For example:

```yaml
gateway:
  privatePeers:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: consumers
      podSelector:
        matchLabels:
          app.kubernetes.io/name: approved-consumer
```

Network access does not replace certificate and application authorization.
Use `lokosAdapter.privatePeers` when an approved gateway is outside this
release. The policy leaves outbound connections available for configured
dependencies. Additional namespace policies may constrain them.

For a separately hosted adapter, set `lokosAdapter.enabled=false` and point the
gateway at the approved service. For a fictional fixture, pre-create
`mockLokos.existingSecret` (and its optional `fileSecret`), enable
`mockLokos.enabled=true`, and point the adapter at the mock Service.

Use a new Secret name when rolling out changed runtime config or keys so Helm
updates the pod template. If you update a Secret in place, restart the affected
Deployment so the application reloads it. Back up the databases before changing
images or schema versions. A Helm rollback does not roll back SQL migrations.

## Validate or package

```sh
helm lint infra/helm/gramstack
helm template gramstack infra/helm/gramstack -f cluster-values.yaml
helm package infra/helm/gramstack
```

To remove the Kubernetes workloads, run
`helm uninstall gramstack --namespace gramstack`. The operator-provided databases
and Secrets have their own lifecycle.
