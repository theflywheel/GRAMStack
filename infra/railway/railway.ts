import { defineRailway, project, service } from "railway/iac";

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

  return project("gramstack-design", {
    // Secrets and environment variables stay managed in Railway.
    variables: { managed: false },
    resources: [docs],
  });
});
