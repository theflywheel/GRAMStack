"""Assemble service-scoped runtime file bundles without contacting Railway."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets

RUNTIME = "/run/gramstack/"


def read(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 65536:
        raise ValueError("Expected a regular, bounded input file")
    return path.read_bytes()


def encoded(value):
    return base64.b64encode(value).decode("ascii")


def settings(args):
    certs, config, identity = args.certs, args.config, args.identity

    def files(directory, *names):
        return {name: read(directory / name) for name in names}

    def tls(name):
        return dict(SERVICE_CA_FILE=RUNTIME+"ca.pem",
                    SERVICE_CERT_FILE=RUNTIME+name+".pem",
                    SERVICE_KEY_FILE=RUNTIME+name+".key")

    roles = {
        "gateway": (
            files(certs, "ca.pem", "gateway.pem", "gateway.key", "consent-signing.key",
                  "grant-signing.key", "policy-signing.pub", "adapter-evidence.pub", "consumer-one.pem", "lokos-rights.pem")
            | files(config, "gateway.json", "policy.jws") | files(identity, "esignet-client.pem")
            | {"private-ca.pem": read(args.private_roots), "publisher.key": read(args.publisher_key)},
            tls("gateway") | dict(
                PORT="8080", GRAMSTACK_ENV="demo",
                GRAMSTACK_CONFIG_FILE=RUNTIME+"gateway.json",
                GRAMSTACK_PRIVATE_CA_FILE=RUNTIME+"private-ca.pem",
                GRAMSTACK_CONSENT_SIGNING_KEY_FILE=RUNTIME+"consent-signing.key",
                GRAMSTACK_GRANT_SIGNING_KEY_FILE=RUNTIME+"grant-signing.key",
                GRAMSTACK_UPSTREAM_PRIVATE_KEY_FILE=RUNTIME+"esignet-client.pem",
                GRAMSTACK_AUTH_KEY=encoded(secrets.token_bytes(32)),
                DEDI_NODE_URL="https://dedi.railway.internal:8446",
                DEDI_NAMESPACE="gramstack-demo", DEDI_REGISTRY="commitments",
                DEDI_PUBLISHER_KEY_ID="gramstack-demo", DEDI_ADMIN_USER="admin",
                DEDI_PUBLISHER_KEY_FILE=RUNTIME+"publisher.key",
                DEDI_CHECKPOINT_ORIGIN="gramstack-demo/log",
            ),
        ),
        "lokos-adapter": (
            files(certs, "ca.pem", "lokos-adapter.pem", "lokos-adapter.key",
                  "adapter-evidence.key", "grant-signing.pub") | files(config, "adapter.json"),
            tls("lokos-adapter") | dict(
                PORT="8080", ADAPTER_TLS_PORT="8443", GRAMSTACK_ENV="demo",
                ADAPTER_DEMO_VERIFIER="true", ADAPTER_STATE_KEY=encoded(secrets.token_bytes(32)),
                ADAPTER_RETRIEVAL_CONFIG_FILE=RUNTIME+"adapter.json",
                ADAPTER_EVIDENCE_SIGNING_KEY_FILE=RUNTIME+"adapter-evidence.key",
                LOKOS_SOURCE_URL="https://mock-lokos.railway.internal:8444",
                LOKOS_SOURCE_SERVER_NAME="mock-lokos.gramstack.internal",
            ),
        ),
        "mock-lokos": (
            files(certs, "ca.pem", "mock-lokos.pem", "mock-lokos.key")
            | files(identity, "esignet-bindings.json"),
            tls("mock-lokos") | dict(
                GRAMSTACK_ENV="demo", SOURCE_TLS_PORT="8444",
                OIDC_ISSUER="https://gramstack-esignet-b9b69819.up.railway.app",
                OIDC_SUBJECT_BINDINGS_FILE=RUNTIME+"esignet-bindings.json",
            ),
        ),
        "sakhicredit": (
            files(certs, "ca.pem", "consumer-one.pem", "consumer-one.key")
            | files(config, "product.json") | {"product-storage.key": secrets.token_bytes(32)},
            dict(PORT="8080", SAKHI_ENV="demo", SAKHI_CONFIG_FILE=RUNTIME+"product.json",
                 SAKHI_STORAGE_KEY_FILE=RUNTIME+"product-storage.key",
                 SAKHI_SERVICE_CA_FILE=RUNTIME+"ca.pem",
                 SAKHI_CONSUMER_CERT_FILE=RUNTIME+"consumer-one.pem",
                 SAKHI_CONSUMER_KEY_FILE=RUNTIME+"consumer-one.key"),
        ),
    }
    for name, (bundle, env) in roles.items():
        value = json.dumps({key: encoded(data) for key, data in bundle.items()}, separators=(",", ":"))
        if len(value) > 120000:
            raise ValueError("Runtime bundle exceeds the deployment environment limit")
        yield name, env | {"GRAMSTACK_FILES_JSON": value}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("certs", "config", "identity", "private-roots", "publisher-key"):
        parser.add_argument("--"+name, type=Path, required=True)
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    try:
        roles = dict(settings(args))
    except (OSError, ValueError):
        parser.error("Check that all required bounded runtime files are present")
    args.directory.mkdir(mode=0o700)
    os.umask(0o077)
    for name, env in roles.items():
        with (args.directory / f".env.{name}.json").open("x", encoding="utf-8") as output:
            json.dump(env, output, indent=2)
            output.write("\n")
    print("Prepared four private service settings files. No secrets were uploaded.")


if __name__ == "__main__":
    main()
