# Build from the repository root. Independent service image; no runtime compiler.
FROM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS build
WORKDIR /src
COPY go.work ./
COPY services/gateway/ services/gateway/
COPY sdk/go/adapter/ sdk/go/adapter/
COPY adapters/auth/oidc/ adapters/auth/oidc/
COPY adapters/lokos/ adapters/lokos/
COPY mocks/identity/ mocks/identity/
COPY mocks/lokos/ mocks/lokos/
COPY apps/sakhicredit/ apps/sakhicredit/
COPY tools/runtimefiles/main.go tools/runtimefiles/main.go
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/service ./services/gateway/cmd/gramstack && \
    GOWORK=off CGO_ENABLED=0 go build -trimpath -o /out/runtimefiles ./tools/runtimefiles/main.go

FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN mkdir -p /app /run/gramstack && chown 10001:10001 /run/gramstack && chmod 0700 /run/gramstack
COPY --from=build /out/service /out/runtimefiles /app/
COPY services/gateway/start.sh /app/start-gateway
USER 10001:10001
WORKDIR /app
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/app/runtimefiles"]
CMD ["/app/start-gateway"]
