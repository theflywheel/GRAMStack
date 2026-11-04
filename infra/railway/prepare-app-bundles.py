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


def encryption_keys(previous):
    if previous is None:
        return tuple(secrets.token_bytes(32) for _ in range(3))

    def settings_file(role):
        path = previous / f".env.{role}.json"
        if path.is_symlink() or not path.is_file():
            raise ValueError("Expected a private previous settings file")
        info = path.stat()
        if info.st_uid != os.getuid() or info.st_mode & 0o077 or info.st_size > 131072:
            raise ValueError("Expected a private previous settings file")
        return json.loads(path.read_bytes())

    gateway = settings_file("gateway")["GRAMSTACK_AUTH_KEY"]
    adapter = settings_file("lokos-adapter")["ADAPTER_STATE_KEY"]
    product = json.loads(settings_file("sakhicredit")["GRAMSTACK_FILES_JSON"])["product-storage.key"]
    keys = tuple(base64.b64decode(value, validate=True) for value in (gateway, adapter, product))
    if any(len(key) != 32 for key in keys) or len(set(keys)) != 3:
        raise ValueError("Expected three distinct 32-byte storage encryption keys")
    return keys


def check_runtime_references(value, bundle):
    if isinstance(value, dict):
        values = value.values()
    elif isinstance(value, list):
        values = value
    else:
        if isinstance(value, str) and value.startswith(RUNTIME) and value[len(RUNTIME):] not in bundle:
            raise ValueError("Runtime file reference is absent from its service bundle")
        return
    for entry in values:
        check_runtime_references(entry, bundle)


def settings(args):
    certs, config, identity = args.certs, args.config, args.identity
    auth_key, adapter_key, product_key = encryption_keys(args.previous_settings)

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
                GRAMSTACK_AUTH_KEY=encoded(auth_key),
                DEDI_NODE_URL="https://dedi.railway.internal:8446",
                DEDI_NAMESPACE="gramstack", DEDI_REGISTRY="commitments",
                DEDI_PUBLISHER_KEY_ID="gramstack-gateway", DEDI_ADMIN_USER="admin",
                DEDI_PUBLISHER_KEY_FILE=RUNTIME+"publisher.key",
                DEDI_CHECKPOINT_ORIGIN="gramstack/log",
            ),
        ),
        "lokos-adapter": (
            files(certs, "ca.pem", "lokos-adapter.pem", "lokos-adapter.key",
                  "adapter-evidence.key", "grant-signing.pub") | files(config, "adapter.json"),
            tls("lokos-adapter") | dict(
                PORT="8080", ADAPTER_TLS_PORT="8443", GRAMSTACK_ENV="demo",
                ADAPTER_DEMO_VERIFIER="true", ADAPTER_STATE_KEY=encoded(adapter_key),
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
            | files(config, "product.json") | {"product-storage.key": product_key},
            dict(PORT="8080", SAKHI_ENV="demo", SAKHI_CONFIG_FILE=RUNTIME+"product.json",
                 SAKHI_STORAGE_KEY_FILE=RUNTIME+"product-storage.key",
                 SAKHI_SERVICE_CA_FILE=RUNTIME+"ca.pem",
                 SAKHI_CONSUMER_CERT_FILE=RUNTIME+"consumer-one.pem",
                 SAKHI_CONSUMER_KEY_FILE=RUNTIME+"consumer-one.key"),
        ),
    }
    for name, (bundle, env) in roles.items():
        check_runtime_references(env, bundle)
        for filename in ("gateway.json", "adapter.json", "product.json"):
            if filename in bundle:
                check_runtime_references(json.loads(bundle[filename]), bundle)
        value = json.dumps({key: encoded(data) for key, data in bundle.items()}, separators=(",", ":"))
        if len(value) > 120000:
            raise ValueError("Runtime bundle exceeds the deployment environment limit")
        yield name, env | {"GRAMSTACK_FILES_JSON": value}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("certs", "config", "identity", "private-roots", "publisher-key"):
        parser.add_argument("--"+name, type=Path, required=True)
    parser.add_argument("--previous-settings", type=Path,
                        help="preserve encryption keys from a prior private bundle directory")
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    try:
        roles = dict(settings(args))
    except (OSError, ValueError, KeyError, TypeError):
        parser.error("Check the required runtime files, references and previous private settings")
    args.directory.mkdir(mode=0o700)
    os.umask(0o077)
    for name, env in roles.items():
        with (args.directory / f".env.{name}.json").open("x", encoding="utf-8") as output:
            json.dump(env, output, indent=2)
            output.write("\n")
    print("Prepared four private service settings files. No secrets were uploaded.")


if __name__ == "__main__":
    main()
