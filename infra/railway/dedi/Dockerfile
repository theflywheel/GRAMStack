FROM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS build
RUN curl --fail --silent --show-error --location --max-time 60 \
    https://codeload.github.com/theflywheel/DeDi-node/tar.gz/dbdb9f6ce8f69b8253f405f9c14ea345cd9e6d1f -o /tmp/dedi.tar.gz && \
    echo '372cea64e2f8222b5c45a150b5f60235d2699258d8ab7b1be56249d9172f47c6  /tmp/dedi.tar.gz' | sha256sum -c - && \
    mkdir /src && tar -xzf /tmp/dedi.tar.gz --strip-components=1 -C /src
WORKDIR /src
COPY infra/railway/dedi/main.go /tmp/dedi-tls.go
COPY tools/runtimefiles/main.go /tmp/runtimefiles.go
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly -trimpath -o /out/dedid ./cmd/dedid && \
    CGO_ENABLED=0 go build -trimpath -o /out/dedi-tls /tmp/dedi-tls.go && \
    CGO_ENABLED=0 go build -trimpath -o /out/runtimefiles /tmp/runtimefiles.go
FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
RUN mkdir -p /app /run/gramstack && chown 10001:10001 /run/gramstack && chmod 0700 /run/gramstack
COPY --from=build /out/ /app/
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 10001:10001
WORKDIR /app
ENV PORT=8080
ENTRYPOINT ["/app/runtimefiles"]
CMD ["/app/dedi-tls"]
