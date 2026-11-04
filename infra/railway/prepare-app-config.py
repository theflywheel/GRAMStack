"""Prepare runtime JSON for the Railway demo after private eSignet onboarding."""
import argparse
import json
import os
from pathlib import Path

ISSUER = "https://gramstack-esignet-b9b69819.up.railway.app"
GATEWAY = "https://gramstack-gateway-b9b69819.up.railway.app"
PRODUCT = "https://sakhicredit-b9b69819.up.railway.app"
HYDRA = "https://gramstack-hydra-b9b69819.up.railway.app"
HYDRA_ADMIN = "https://hydra-admin.railway.internal:4445"
DATA = "https://gateway.railway.internal:8445"
ADAPTER = "https://lokos-adapter.railway.internal:8443"
METADATA = "https://gramstack-adapter-b9b69819.up.railway.app"
RUNTIME = "/run/gramstack/"
FIELDS = ["membership_status", "savings_summary"]
TERMS = (
    "Use only for this credit eligibility assessment. Retain approved data for up to "
    "30 days, then delete it unless a documented legal retention requirement applies. "
    "No onward sharing."
)


def configs(provider):
    required = {
        "issuer": ISSUER,
        "schema_version": "gramstack.auth.oidc/v1",
        "token_endpoint_auth_method": "private_key_jwt",
        "assertion_key_id": "client-key",
        "assertion_audience": "issuer",
        "id_token_algorithms": ["PS256"],
        "jwks_certificate_metadata": "ignore",
    }
    if not isinstance(provider, dict) or set(provider) != set(required) | {"client_id"}:
        raise ValueError("Expected an onboarded eSignet demo provider profile")
    if any(provider[key] != value for key, value in required.items()):
        raise ValueError("Provider profile does not match the Railway eSignet issuer and protocol")
    if not isinstance(provider["client_id"], str) or not provider["client_id"]:
        raise ValueError("Provider profile requires a client ID")

    policy = dict(registry="lokos", version="demo-policy-v1", consumer_id="consumer-one",
                  resource="shg.membership", purpose="credit_eligibility", fields=FIELDS,
                  require_verified_subject=True, require_human_consent=True,
                  max_grant_seconds=60, max_disclosures=1)
    gateway = dict(
        public_origin=GATEWAY, hydra_introspection_url=HYDRA_ADMIN + "/admin/oauth2/introspect",
        hydra_issuer=HYDRA + "/", audience="gramstack", scope="gramstack.access",
        allow_loopback_http=False,
        consumers={"consumer-one": [dict(registry="lokos", resource="shg.membership", fields=FIELDS,
                    purposes=["credit_eligibility"], hint_types=["member_reference"],
                    terms_version="terms-v1", max_access_seconds=300, terms_text=TERMS)]},
        authentication=dict(client_id="gramstack-portal", hydra_admin_url=HYDRA_ADMIN,
                            provider=provider, demo_upstream=True),
        registries=[dict(registry="lokos", origin=ADAPTER, server_name="lokos-adapter.gramstack.internal",
                         verification_method="demo.registry-map-v1", verification_contract_version="0.1.0",
                         demo=True, discovery=dict(version="0.1.0", origin=METADATA, resources=[
                             dict(name="shg.membership", version="1", fields=FIELDS)]))],
        consent=dict(key_id="demo-consent-key", status_origin=DATA, workers_enabled=True),
        authorization=dict(
            key_id="demo-grant-key", listen_address=":8445",
            policy_keys=[dict(registry="lokos", key_id="demo-policy-key", file=RUNTIME + "policy-signing.pub")],
            policies=[dict(file=RUNTIME + "policy.jws", expected_version="")],
            adapters=[dict(registry="lokos", audience="lokos-adapter", adapter_id="lokos-adapter",
                           service_identity="lokos-adapter.gramstack.internal", evidence_keys=[
                               dict(key_id="demo-adapter-key", file=RUNTIME + "adapter-evidence.pub")])],
            presenter_certificates={"consumer-one": RUNTIME + "consumer-one.pem"},
            evidence=dict(release_profile="batch", anchoring_enabled=True),
            retrieval=dict(max_concurrent=4),
        ),
    )
    adapter = dict(grant_issuer=GATEWAY, grant_audience="lokos-adapter",
                   grant_keys=[dict(key_id="demo-grant-key", file=RUNTIME + "grant-signing.pub")],
                   authority_origin=DATA, authority_service_identity="gateway.gramstack.internal",
                   evidence_key_id="demo-adapter-key", release_profile="batch", max_concurrent=4)
    product = dict(public_origin=PRODUCT, gateway_origin=GATEWAY, gateway_data_origin=DATA,
                   hydra_token_url=HYDRA + "/oauth2/token", client_id="consumer-one",
                   data_server_name="gateway.gramstack.internal", allow_loopback_http=False)
    return {"gateway.json": gateway, "adapter.json": adapter,
            "product.json": product, "policy.json": policy}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--provider", type=Path, required=True)
    parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    try:
        values = configs(json.loads(args.provider.read_text(encoding="utf-8")))
    except (ValueError, OSError):
        parser.error("Supply the valid Railway eSignet profile produced by private onboarding")
    args.directory.mkdir(mode=0o700)
    os.umask(0o077)
    for name, value in values.items():
        with (args.directory / name).open("x", encoding="utf-8") as output:
            json.dump(value, output, indent=2)
            output.write("\n")
    print("Prepared gateway, adapter, product and policy configuration.")


if __name__ == "__main__":
    main()
