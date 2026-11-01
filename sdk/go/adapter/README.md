# GRAMStack adapter Go SDK

Module: `github.com/theflywheel/GRAMStack/sdk/go/adapter`.

The SDK owns reusable adapter protocol processing. Current primitives are the provider/resource extension point and strict requested-field projection. LokOS imports these types and executes through `ReadSelected` in its tests.

`ReadSelected` is not an authorization boundary. The grant/mTLS, durable redemption/attempt and evidence-before-release pipeline must admit a request before calling it. There is deliberately no unauthenticated business-data HTTP route in the initial executables.

Use the root Go workspace for local SDK + LokOS development. Published SDK tags use the module prefix `sdk/go/adapter/vX.Y.Z`. The SDK must remain independent of gateway internals and registry implementation code.
