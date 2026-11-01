# LokOS reference adapter

Independent Go module using `github.com/theflywheel/GRAMStack/sdk/go/adapter`. The provider implements SDK extension points and its projection tests call the SDK itself, rather than duplicating SDK processing. Source transport and approved subject resolution are supplied separately; no native LokOS API shape is assumed.

Run `go run ./cmd/lokos-adapter` for the process shell. Only liveness and unavailable-readiness endpoints are enabled until the protected SDK pipeline is wired. No production registry credentials or source data are included.
