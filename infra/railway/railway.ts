import { defineRailway, image, project, service, volume, type ServiceConfigInput } from "railway/iac";

// Keep every active service in this project definition; omission removes a resource.
export default defineRailway(() => {
  const docs = service("docs", {
    // No source declaration: docs currently deploys through targeted CLI uploads.
    build: {
      builder: "DOCKERFILE",
      buildEnvironment: "V3",
      dockerfilePath: "docs/Dockerfile",
      watchPatterns: [
        "/docs/**",
        "/specs/**",
        "/infra/railway/**",
        "/.dockerignore",
        "/.railwayignore",
      ],
    },
    healthcheck: "/healthz",
    healthcheckTimeout: 30,
    replicas: { "asia-southeast1-eqsg3a": 1 },
    deploy: { restartPolicyMaxRetries: 3 },
    domains: [{ domain: "docs.gramstack.try-dough.com", port: 8080 }],
    // Retain the existing private hostname so this migration does not change routing.
    networking: { privateNetworkEndpoint: "design-site" },
  });

  const platformData = volume("platform-data", { region: "asia-southeast1-eqsg3a", sizeMB: 50000 });
  const platformDatabase = service("platform-db", {
    build: {
      builder: "DOCKERFILE",
      buildEnvironment: "V3",
      dockerfilePath: "infra/railway/database/Dockerfile",
      watchPatterns: ["/infra/railway/database/**", "/infra/esignet/init.sql"],
    },
    replicas: { "asia-southeast1-eqsg3a": 1 },
    deploy: { restartPolicyMaxRetries: 3 },
    volumeMounts: { "/var/lib/postgresql/data": platformData },
    // Railway derives platform-db.railway.internal from the service name.
  });

  const hydraImage = image("oryd/hydra:v26.2.0@sha256:ff67c7fb5f95074fa53374d41151713554960504b340cd3f95b09e65deaea2a9", {
    autoUpdates: { type: "disabled" },
  });
  const hydraAdmin = service("hydra-admin", {
    source: hydraImage,
    start: "hydra serve admin --sqa-opt-out",
    preDeploy: ["hydra migrate sql up -e -y"],
    replicas: { "asia-southeast1-eqsg3a": 1 },
    deploy: { restartPolicyMaxRetries: 3 },
  });
  const hydraPublic = service("hydra-public", {
    source: hydraImage,
    start: "hydra serve public --sqa-opt-out",
    healthcheck: "/health/ready",
    healthcheckTimeout: 60,
    replicas: { "asia-southeast1-eqsg3a": 1 },
    deploy: { restartPolicyMaxRetries: 3 },
    networking: {
      serviceDomains: { "gramstack-hydra-b9b69819.up.railway.app": { port: 4444 } },
    },
  });

  const dedi = service("dedi", {
    build: {
      builder: "DOCKERFILE",
      buildEnvironment: "V3",
      dockerfilePath: "infra/railway/dedi/Dockerfile",
      watchPatterns: ["/infra/railway/dedi/**", "/tools/runtimefiles/**"],
    },
    healthcheck: "/healthz",
    healthcheckTimeout: 60,
    replicas: { "asia-southeast1-eqsg3a": 1 },
    deploy: { restartPolicyMaxRetries: 3 },
  });

  const identityRegion = "asia-southeast1-eqsg3a";
  const identityVolume = (name: string, sizeMB: number) => volume(name, { region: identityRegion, sizeMB });
  const identityArtifact = (name: string, settings: ServiceConfigInput) => service(name, {
    build: {
      builder: "DOCKERFILE",
      buildEnvironment: "V3",
      dockerfilePath: `infra/railway/${name}/Dockerfile`,
      watchPatterns: [`/infra/railway/${name}/**`, "/infra/esignet/**"],
    },
    replicas: { [identityRegion]: 1 },
    deploy: { restartPolicyMaxRetries: 3 },
    ...settings,
  });
  const esignetRedis = identityArtifact("esignet-redis", {
    volumeMounts: { "/data": identityVolume("esignet-redis-data", 512) },
  });
  const esignetMock = identityArtifact("esignet-mock", {
    build: {
      builder: "DOCKERFILE",
      buildEnvironment: "V3",
      dockerfilePath: "infra/railway/esignet-mock/Dockerfile",
      watchPatterns: ["/infra/railway/esignet-mock/**", "/infra/railway/esignet/keystore-start.sh"],
    },
    volumeMounts: { "/keys": identityVolume("esignet-mock-keys", 512) },
  });
  const esignet = identityArtifact("esignet", {
    healthcheck: "/health",
    healthcheckTimeout: 120,
    volumeMounts: { "/opt/mosip/keys": identityVolume("esignet-keys", 512) },
  });
  const esignetUI = identityArtifact("esignet-ui", {
    healthcheck: "/healthz",
    healthcheckTimeout: 120,
    networking: {
      serviceDomains: { "gramstack-esignet-b9b69819.up.railway.app": { port: 3000 } },
    },
  });

  const application = (name: string, path: string, publicHost?: string) => service(name, {
    build: {
      builder: "DOCKERFILE",
      buildEnvironment: "V3",
      dockerfilePath: `${path}/Dockerfile`,
    },
    replicas: { [identityRegion]: 1 },
    deploy: { restartPolicyMaxRetries: 3 },
    ...(publicHost ? {
      healthcheck: "/readyz",
      healthcheckTimeout: 30,
      preDeploy: ["/app/runtimefiles /app/service migrate"],
      networking: { serviceDomains: { [publicHost]: { port: 8080 } } },
    } : {}),
  });
  const gateway = application("gateway", "services/gateway", "gramstack-gateway-b9b69819.up.railway.app");
  const adapter = application("lokos-adapter", "adapters/lokos", "gramstack-adapter-b9b69819.up.railway.app");
  const source = application("mock-lokos", "mocks/lokos");
  const product = application("sakhicredit", "apps/sakhicredit", "sakhicredit-b9b69819.up.railway.app");

  return project("gramstack-design", {
    // Secrets and environment variables stay managed in Railway.
    variables: { managed: false },
    resources: [docs, platformDatabase, hydraAdmin, hydraPublic, dedi, esignetRedis, esignetMock, esignet, esignetUI, gateway, adapter, source, product],
  });
});
