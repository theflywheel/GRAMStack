---
title: Building adapters
---

An adapter is an independently publishable package or hosted service implementing the registry contract. The gateway uses configured, approved capabilities and trust; it does not hardcode native registry integration.

## Implement the boundary

- Expose the versioned capability manifest and public verification keys.
- Implement the approved registry subject-verification method and keep native identifiers in protected adapter custody.
- Enforce signed grants, presenter identity and one-use admission before native reads.
- Fetch only approved fields, reject unexpected projection and disable implicit native retries.
- Submit signed evidence to durable custody before protected release.

Go implementations can use `github.com/theflywheel/GRAMStack/sdk/go/adapter`. The reference LokOS adapter is built with this SDK. Implementations in other languages can follow the same HTTP contract.

The SDK provides verification/retrieval handlers, durable attempt storage and conformance helpers. The host supplies the source provider, trusted keys and mTLS clients, stable state encryption key, configuration and database migrations. Passing a provider fixture does not replace full transport, authorization and restart acceptance.

[Go adapter SDK and examples](https://github.com/theflywheel/GRAMStack/tree/main/sdk/go/adapter) · [LokOS reference](https://github.com/theflywheel/GRAMStack/tree/main/adapters/lokos) · [Adapter contracts](/developers/specifications/adapters) · [Conformance](/developers/specifications/conformance)
