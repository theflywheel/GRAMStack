"""Run a fresh, isolated SakhiCredit demo until interrupted."""
import argparse
import base64
from contextlib import ExitStack
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import sys
import time
from urllib.parse import urlencode
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[2]
DEDI_REVISION = "dbdb9f6ce8f69b8253f405f9c14ea345cd9e6d1f"


def origins():
    with ExitStack() as stack:
        ports = {}
        for role in ("pg", "hydra", "admin", "gateway", "data", "adapter",
                     "metadata", "source", "product", "dedi"):
            listener = stack.enter_context(socket.socket())
            listener.bind(("127.0.0.1", 0))
            ports[role] = listener.getsockname()[1]
    return ports


def run(state, pg, hydra, dedi_source):
    if subprocess.check_output(["git", "-C", dedi_source, "rev-parse", "HEAD"], text=True).strip() != DEDI_REVISION:
        raise RuntimeError("Use the pinned DeDi-node revision")
    subprocess.run(["git", "-C", dedi_source, "diff", "--quiet", "HEAD", "--"], check=True)
    version = subprocess.check_output([hydra, "version"], text=True, timeout=5).splitlines()
    if not version or not version[0].endswith(" v26.2.0"):
        raise RuntimeError("Use the official Hydra v26.2.0 executable")
    state.mkdir(mode=0o700)
    os.umask(0o077)
    ports = origins()
    urls = {name: f"http://127.0.0.1:{port}" for name, port in ports.items()}
    for name in ("data", "adapter", "source"):
        urls[name] = urls[name].replace("http:", "https:")
    processes = {}
    compose = None
    # Services receive explicit demo settings only.
    service_env = {"PATH": os.environ["PATH"], "BIND_HOST": "127.0.0.1", "LC_ALL": "C"}

    def command(args, env=None, output=None):
        return subprocess.run(args, cwd=ROOT, env=env, stdout=output, stderr=output, check=True, timeout=300)

    def logged(name, args, env=None):
        with (state / (name + ".log")).open("wb") as log:
            command(args, env, log)

    def start(name, args, env):
        with (state / (name + ".log")).open("wb") as log:
            processes[name] = subprocess.Popen(args, cwd=ROOT, env=service_env | env, stdout=log, stderr=log, start_new_session=True)

    def ready(name, url, seconds=15, tls_args=()):
        until = time.monotonic() + seconds
        while time.monotonic() < until:
            if name in processes and processes[name].poll() is not None:
                break
            if tls_args:
                probe = subprocess.run(["curl", "--fail", "--silent", "--max-time", "2",
                                        *tls_args, url], stdout=subprocess.DEVNULL, timeout=3)
                if probe.returncode == 0:
                    return
            else:
                try:
                    with urlopen(url, timeout=2) as response:
                        if response.status == 200:
                            return
                except OSError:
                    pass
            time.sleep(0.2)
        raise RuntimeError(f"{name} did not become ready. See logs in {state}")

    def config(name, value):
        path = state / name
        path.write_text(json.dumps(value), encoding="utf-8")
        return str(path)

    def secret():
        return secrets.token_hex(32)

    def key():
        return base64.b64encode(secrets.token_bytes(32)).decode()

    def tls(name):
        return dict(SERVICE_CA_FILE=str(certs / "ca.pem"),
                    SERVICE_CERT_FILE=str(certs / (name + ".pem")),
                    SERVICE_KEY_FILE=str(certs / (name + ".key")))

    def db(name):
        return "postgres://gramstack@localhost/" + name + "?" + urlencode(
            dict(host=str(state), port=ports["pg"], sslmode="disable"))

    try:
        command([sys.executable, "infra/esignet/init-demo.py", state / "esignet"])
        e_env = dict(line.split("=", 1) for line in (state / "esignet/.env").read_text().splitlines())
        e_env["ESIGNET_DEMO_ISSUER_PORT"] = e_env["ESIGNET_DEMO_API_PORT"]
        compose = ["docker", "compose", "-f", "infra/esignet/compose.yaml"]
        logged("esignet-compose", compose + ["up", "-d", "--wait", "--wait-timeout", "240"], os.environ | e_env)
        issuer = "http://127.0.0.1:" + e_env["ESIGNET_DEMO_API_PORT"]
        ready("esignet", issuer + "/health", 90)
        command([sys.executable, "infra/esignet/check.py"], os.environ | e_env)

        print("Building demo services and preparing private SQL state", flush=True)
        binaries = {}
        for name, package in {"gateway": "./services/gateway/cmd/gramstack",
                              "adapter": "./adapters/lokos/cmd/lokos-adapter",
                              "source": "./mocks/lokos/cmd/mock-lokos",
                              "product": "./apps/sakhicredit/cmd/sakhicredit"}.items():
            binaries[name] = str(state / name)
            logged("build-" + name, ["go", "build", "-mod=readonly", "-o", binaries[name], package], os.environ | {"GOWORK": str(ROOT / "go.work")})
        dedid = str(state / "dedid")
        logged("build-dedi", ["go", "-C", dedi_source, "build", "-mod=readonly", "-o", dedid, "./cmd/dedid"], os.environ | {"GOWORK": "off"})
        certs = state / "certs"
        logged("certificates", ["go", "run", "./tools/devcerts/main.go", "-dir", certs], os.environ | {"GOWORK": "off"})
        logged("postgres-init", [pg / "initdb", "-D", state / "pg", "-U", "gramstack",
                                "--auth-local=trust", "--auth-host=scram-sha-256", "--locale=C", "--encoding=UTF8"])
        start("postgres", [pg / "postgres", "-D", state / "pg", "-k", state,
                           "-p", str(ports["pg"]), "-c", "listen_addresses="], {})
        for _ in range(100):
            probe = subprocess.run([pg / "pg_isready", "-q", "-U", "gramstack", "-h", state, "-p", str(ports["pg"])], timeout=3)
            if probe.returncode == 0:
                break
            if processes["postgres"].poll() is not None:
                raise RuntimeError("PostgreSQL exited. See postgres.log")
            time.sleep(0.1)
        else:
            raise RuntimeError("PostgreSQL did not become ready")
        for name in ("core", "adapter", "product", "hydra", "dedi"):
            command([pg / "createdb", "-h", state, "-p", str(ports["pg"]), "-U", "gramstack", name])

        h_env = dict(DSN=db("hydra"), SECRETS_SYSTEM=secret(), LOG_LEVEL="error",
                     URLS_SELF_ISSUER=urls["hydra"]+"/", URLS_LOGIN=urls["gateway"]+"/auth/login",
                     URLS_CONSENT=urls["gateway"]+"/auth/oauth-consent",
                     SERVE_PUBLIC_HOST="127.0.0.1", SERVE_PUBLIC_PORT=str(ports["hydra"]),
                     SERVE_ADMIN_HOST="127.0.0.1", SERVE_ADMIN_PORT=str(ports["admin"]))
        logged("hydra-migrate", [hydra, "migrate", "sql", "up", "-e", "-y"], service_env | h_env)
        start("hydra", [hydra, "serve", "all", "--dev", "--sqa-opt-out"], h_env)
        ready("hydra", urls["admin"]+"/health/ready")
        consumer_secret, portal_secret = secret(), secret()
        for client in (dict(client_id="consumer-one", client_secret=consumer_secret,
                            grant_types=["client_credentials"], scope="gramstack.access",
                            audience=["gramstack"], token_endpoint_auth_method="client_secret_basic"),
                       dict(client_id="gramstack-portal", client_secret=portal_secret,
                            grant_types=["authorization_code"], response_types=["code"], scope="openid",
                            redirect_uris=[urls["gateway"]+"/auth/callback"],
                            token_endpoint_auth_method="client_secret_basic")):
            with urlopen(Request(urls["admin"]+"/admin/clients", json.dumps(client).encode(),
                                 {"Content-Type": "application/json"}), timeout=10) as response:
                if response.status != 201:
                    raise RuntimeError("Hydra client provisioning failed")

        logged("esignet-onboard", ["go", "-C", "adapters/auth/oidc", "run",
                                  ROOT / "tools/esignet-demo/main.go", ROOT / "tools/esignet-demo/bindings.go",
                                  "-callback", urls["gateway"]+"/auth/provider/callback", "-directory", state],
               os.environ | e_env | {"GOWORK": "off", "GRAMSTACK_ENV": "development"})

        node_key = state / "node.key"
        publisher = state / "publisher.key"
        checkpoint = subprocess.check_output([dedid, "keygen", "-out", node_key, "-name", "gramstack-demo"], text=True).splitlines()[-1]
        (state / "checkpoint.txt").write_text(checkpoint + "\n")
        publisher_keys = subprocess.check_output([dedid, "pubkeygen", "-out", publisher, "-kid", "gramstack-demo",
                                                   "-namespace", "gramstack-demo"], text=True)
        keys = next(line.split("=", 1)[1] for line in publisher_keys.splitlines() if line.startswith("DEDI_PUBLISHER_KEYS="))
        password = secret()
        start("dedi", [dedid, "serve"], dict(DEDI_DB_URL=db("dedi"), DEDI_KEY_FILE=str(node_key),
              DEDI_ORIGIN="gramstack-demo/log", DEDI_PUBLISHER_KEYS=keys, DEDI_ADMIN_PASSWORD=password,
              DEDI_LISTEN=f"127.0.0.1:{ports['dedi']}"))
        ready("dedi", urls["dedi"]+"/healthz")

        fields = ["membership_status", "savings_summary"]
        policy_key = dict(key_id="demo-policy-key", file=str(certs / "policy-signing.pub"), registry="lokos")
        grant_key = dict(key_id="demo-grant-key", file=str(certs / "grant-signing.pub"))
        evidence_key = dict(key_id="demo-adapter-key", file=str(certs / "adapter-evidence.pub"))
        policy = config("policy.json", dict(registry="lokos", version="demo-policy-v1", consumer_id="consumer-one",
                        resource="shg.membership", purpose="credit_eligibility", fields=fields, require_verified_subject=True,
                        require_human_consent=True, max_grant_seconds=60, max_disclosures=1))
        signed_policy = state / "policy.jws"
        logged("policy-sign", [binaries["gateway"], "sign-policy", "-input", policy, "-key", certs / "policy-signing.key",
                               "-key-id", "demo-policy-key", "-output", signed_policy])
        gateway_config = config("gateway.json", dict(
            public_origin=urls["gateway"], hydra_introspection_url=urls["admin"]+"/admin/oauth2/introspect",
            hydra_issuer=urls["hydra"]+"/", audience="gramstack", scope="gramstack.access", allow_loopback_http=True,
            consumers={"consumer-one": [dict(registry="lokos", resource="shg.membership", fields=fields,
                       purposes=["credit_eligibility"], hint_types=["member_reference"], terms_version="terms-v1",
                       terms_text="Use only for this credit eligibility assessment. Retain approved data for up to 30 days, then delete it unless a documented legal retention requirement applies. No onward sharing.")]},
            authentication=dict(client_id="gramstack-portal", hydra_admin_url=urls["admin"],
                                provider=json.loads((state / "esignet-profile.json").read_text()), demo_upstream=True),
            registries=[dict(registry="lokos", origin=urls["adapter"], server_name="lokos-adapter.gramstack.internal",
                             verification_method="demo.registry-map-v1", verification_contract_version="0.1.0", demo=True,
                             discovery=dict(version="0.1.0", origin=urls["metadata"], resources=[
                                 dict(name="shg.membership", version="1", fields=fields)]))],
            consent=dict(key_id="demo-consent-key", status_origin=urls["data"], workers_enabled=True),
            authorization=dict(key_id="demo-grant-key", listen_address=f"127.0.0.1:{ports['data']}",
                               policy_keys=[policy_key], policies=[dict(file=str(signed_policy), expected_version="")],
                               adapters=[dict(registry="lokos", audience="lokos-adapter", adapter_id="lokos-adapter",
                                              service_identity="lokos-adapter.gramstack.internal", evidence_keys=[evidence_key])],
                               presenter_certificates={"consumer-one": str(certs / "consumer-one.pem")},
                               evidence=dict(release_profile="batch", anchoring_enabled=True), retrieval=dict(max_concurrent=4))))
        gateway_env = tls("gateway") | dict(
            PORT=str(ports["gateway"]), GRAMSTACK_ENV="development", DATABASE_URL=db("core"),
            GRAMSTACK_CONFIG_FILE=gateway_config, GRAMSTACK_AUTH_KEY=key(), GRAMSTACK_PORTAL_CLIENT_SECRET=portal_secret,
            GRAMSTACK_UPSTREAM_PRIVATE_KEY_FILE=str(state / "esignet-client.pem"),
            GRAMSTACK_CONSENT_SIGNING_KEY_FILE=str(certs / "consent-signing.key"),
            GRAMSTACK_GRANT_SIGNING_KEY_FILE=str(certs / "grant-signing.key"), DEDI_NODE_URL=urls["dedi"],
            DEDI_NAMESPACE="gramstack-demo", DEDI_REGISTRY="commitments", DEDI_PUBLISHER_KEY_ID="gramstack-demo",
            DEDI_PUBLISHER_KEY_FILE=str(publisher), DEDI_ADMIN_USER="admin", DEDI_ADMIN_PASSWORD=password,
            DEDI_CHECKPOINT_ORIGIN="gramstack-demo/log", DEDI_CHECKPOINT_VERIFIER=checkpoint)
        logged("dedi-provision", [binaries["gateway"], "anchor", "-development-loopback", "-provision"],
               service_env | gateway_env)
        adapter_env = tls("lokos-adapter") | dict(
            PORT=str(ports["metadata"]), ADAPTER_TLS_PORT=str(ports["adapter"]), GRAMSTACK_ENV="development",
            ADAPTER_DEMO_VERIFIER="true", ADAPTER_STATE_DATABASE_URL=db("adapter"), ADAPTER_STATE_KEY=key(),
            LOKOS_SOURCE_URL=urls["source"], LOKOS_SOURCE_SERVER_NAME="mock-lokos.gramstack.internal",
            ADAPTER_EVIDENCE_SIGNING_KEY_FILE=str(certs / "adapter-evidence.key"),
            ADAPTER_RETRIEVAL_CONFIG_FILE=config("adapter.json", dict(
                grant_issuer=urls["gateway"], grant_audience="lokos-adapter", grant_keys=[grant_key],
                authority_origin=urls["data"], authority_service_identity="gateway.gramstack.internal",
                evidence_key_id="demo-adapter-key", release_profile="batch", max_concurrent=4)))
        storage = state / "product-storage.key"
        storage.write_bytes(secrets.token_bytes(32))
        product_env = dict(PORT=str(ports["product"]), SAKHI_ENV="development", SAKHI_DATABASE_URL=db("product"),
                           SAKHI_CLIENT_SECRET=consumer_secret, SAKHI_STORAGE_KEY_FILE=str(storage),
                           SAKHI_SERVICE_CA_FILE=str(certs / "ca.pem"), SAKHI_CONSUMER_CERT_FILE=str(certs / "consumer-one.pem"),
                           SAKHI_CONSUMER_KEY_FILE=str(certs / "consumer-one.key"),
                           SAKHI_CONFIG_FILE=config("product.json", dict(public_origin=urls["product"], gateway_origin=urls["gateway"],
                               gateway_data_origin=urls["data"], hydra_token_url=urls["hydra"]+"/oauth2/token",
                               client_id="consumer-one", data_server_name="gateway.gramstack.internal", allow_loopback_http=True)))
        for name, env in (("gateway", gateway_env), ("adapter", adapter_env), ("product", product_env)):
            logged(name+"-migrate", [binaries[name], "migrate"], service_env | env)
        start("source", [binaries["source"]], tls("mock-lokos") | dict(
            GRAMSTACK_ENV="development", OIDC_ISSUER=issuer, SOURCE_TLS_PORT=str(ports["source"]),
            OIDC_SUBJECT_BINDINGS_FILE=str(state / "esignet-bindings.json")))
        ready("source", urls["source"]+"/healthz", tls_args=(
            "--cacert", certs / "ca.pem", "--cert", certs / "lokos-adapter.pem",
            "--key", certs / "lokos-adapter.key"))
        for name, env, origin in (("adapter", adapter_env, "metadata"), ("gateway", gateway_env, "gateway"),
                                  ("product", product_env, "product")):
            start(name, [binaries[name]], env)
            ready(name, urls[origin]+"/readyz")
        config("endpoints.json", urls)
        print(f"SakhiCredit is ready at {urls['product']}. Press Ctrl-C to stop.", flush=True)
        while True:
            for name, process in processes.items():
                if process.poll() is not None:
                    raise RuntimeError(f"{name} exited. See {state / (name + '.log')}")
            time.sleep(1)
    finally:
        for process in reversed(list(processes.values())):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
        if compose is not None:
            logged("esignet-stop", compose + ["down"], os.environ | e_env)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("state", type=Path, help="new private state directory under /tmp")
    parser.add_argument("--pg-bin", type=Path, required=True)
    parser.add_argument("--hydra", type=Path, required=True)
    parser.add_argument("--dedi-source", type=Path, required=True)
    args = parser.parse_args()
    state = args.state.resolve()
    if not state.is_relative_to(Path("/tmp").resolve()) or len(str(state)) > 65:
        parser.error("use a short, new state path under /tmp for PostgreSQL's private Unix socket")
    signal.signal(signal.SIGTERM, lambda *_: signal.raise_signal(signal.SIGINT))
    try:
        run(state, args.pg_bin.resolve(), args.hydra.resolve(), args.dedi_source.resolve())
    except KeyboardInterrupt:
        print("Demo stopped. Private state and eSignet volumes retained.", flush=True)
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"Demo setup failed ({type(error).__name__}). See private logs in {state}.", file=sys.stderr)
        if isinstance(error, RuntimeError):
            print(error, file=sys.stderr)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
