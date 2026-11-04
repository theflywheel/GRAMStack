"""Local infrastructure smoke check; does not claim successful user authentication."""
import json
import os
from urllib.request import urlopen


def get(url):
    with urlopen(url, timeout=10) as response:
        if response.status != 200:
            raise RuntimeError(f"Unexpected HTTP status for {url}")
        return response.read()


def origin(kind, default):
    value = os.environ.get(f"ESIGNET_DEMO_{kind}_PORT") or default
    port = int(value)
    if not 1 <= port <= 65535 or str(port) != value:
        raise ValueError(f"Invalid {kind} port")
    return f"http://127.0.0.1:{port}"


def check():
    issuer = origin("API", "19080")
    get(issuer + "/health")
    discovery = json.loads(get(issuer + "/.well-known/openid-configuration"))
    expected = {
        "issuer": issuer,
        "authorization_endpoint": issuer + "/oauth2/authorize",
        "token_endpoint": issuer + "/oauth2/token",
    }
    for key, value in expected.items():
        if discovery.get(key) != value:
            raise RuntimeError(f"Unexpected discovery {key}")
    for key, value in {
        "token_endpoint_auth_methods_supported": "private_key_jwt",
        "id_token_signing_alg_values_supported": "PS256",
    }.items():
        if value not in discovery.get(key, []):
            raise RuntimeError(f"Missing discovery capability: {value}")
    health = json.loads(get(origin("MOCK", "19082") + "/v1/mock-identity-system/actuator/health"))
    if health.get("status") != "UP":
        raise RuntimeError("Mock identity backend is not ready")
    get(origin("UI", "19300") + "/")
    print("PASS: eSignet health, pinned issuer/endpoints, private_key_jwt/PS256, mock backend and UI")


if __name__ == "__main__":
    check()
