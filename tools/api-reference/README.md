# Service-hosted API reference

Each Go API serves its own Swagger UI at `/swagger/` and a primary bundled
OpenAPI document at `/swagger/openapi.json`. The gateway includes its public
and private catalogues and the core protocol. The adapter and product each
publish their own catalogue. All assets and schema references are local.

The service Dockerfiles build these static files in a Node build stage and copy
them into the Go runtime image. `GRAMSTACK_SWAGGER_DIR` selects that directory.
For local development, run `npm ci --ignore-scripts`, then
`node build.mjs gateway` (or `adapter` or `sakhicredit`) here and set
`GRAMSTACK_SWAGGER_DIR` to that service's absolute `public/<service>` path.

Run `node verify-routes.mjs` to compare implemented business routes with the
runtime OpenAPI catalogue. Public request execution remains disabled.
