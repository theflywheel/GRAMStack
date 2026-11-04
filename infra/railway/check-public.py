"""Check the production demo's public HTTPS readiness and route isolation."""
from concurrent.futures import ThreadPoolExecutor
import json
import sys
from urllib.error import HTTPError, URLError
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

DOCS = "https://docs.gramstack.try-dough.com"
HYDRA = "https://gramstack-hydra-b9b69819.up.railway.app"
ESIGNET = "https://gramstack-esignet-b9b69819.up.railway.app"
GATEWAY = "https://gramstack-gateway-b9b69819.up.railway.app"
ADAPTER = "https://gramstack-adapter-b9b69819.up.railway.app"
PRODUCT = "https://sakhicredit-b9b69819.up.railway.app"


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def request(origin, path, method="GET"):
    opener = build_opener(ProxyHandler({}), NoRedirect())
    try:
        with opener.open(Request(origin + path, method=method), timeout=6) as response:
            body = response.read(131073)
            return response.status, response.headers, body if len(body) <= 131072 else b""
    except HTTPError as error:
        with error:
            return error.code, {}, b""
    except (URLError, TimeoutError, OSError):
        return None, {}, b""


def validate(kind, origin, status, headers, body):
    if status != 200:
        return False
    if kind == "ok":
        return body.strip() == b"ok"
    if kind == "ready":
        return body.strip() == b"ready"
    try:
        data = json.loads(body)
    except (ValueError, UnicodeError):
        return False
    if not isinstance(data, dict):
        return False
    if kind in {"hydra_health", "product_ready"}:
        return data.get("status") == ("ok" if kind == "hydra_health" else "ready")
    if kind in {"hydra", "esignet"}:
        expected_issuer = origin + "/" if kind == "hydra" else origin
        authorization_path = "/oauth2/auth" if kind == "hydra" else "/oauth2/authorize"
        keys_path = "/.well-known/jwks.json" if kind == "hydra" else "/oauth2/jwks"
        if data.get("issuer") != expected_issuer or any(
            data.get(key) != origin + path for key, path in {
                "authorization_endpoint": authorization_path,
                "jwks_uri": keys_path,
                "token_endpoint": "/oauth2/token",
            }.items()
        ):
            return False
        if kind == "hydra":
            return True
        methods = data.get("token_endpoint_auth_methods_supported")
        algorithms = data.get("id_token_signing_alg_values_supported")
        return (isinstance(methods, list) and "private_key_jwt" in methods
                and isinstance(algorithms, list) and "PS256" in algorithms)
    return (
        headers.get("Gramstack-Adapter-Discovery-Version") == "0.1.0"
        and headers.get("Gramstack-Contract-Version") == "0.3.0"
        and headers.get("Cache-Control") == "no-store"
        and data.get("registry") == "lokos"
        and data.get("adapter_id") == "lokos-adapter"
        and data.get("authentication") == "mtls"
    )


def check(service):
    name, origin, checks, protected = service
    results = {}
    for label, path, kind in checks:
        status, headers, body = request(origin, path)
        results[label] = {"http": status, "passed": validate(kind, origin, status, headers, body)}
    ready = all(result["passed"] for result in results.values())
    for label, path, method in protected:
        status = request(origin, path, method)[0] if ready else None
        results[label] = {"http": status, "passed": status == 404 if ready else None}
    return name, results


def main():
    discovery = "/.well-known/openid-configuration"
    services = [
        ("docs", DOCS, [("health", "/healthz", "ok")], []),
        ("hydra", HYDRA, [("ready", "/health/ready", "hydra_health"), ("issuer", discovery, "hydra")],
         [("admin_hidden", "/admin/clients", "GET")]),
        ("esignet", ESIGNET, [("health", "/healthz", "ok"), ("issuer", discovery, "esignet")],
         [("management_hidden", "/v1/esignet/client-mgmt/oidc-client", "GET")]),
        ("gateway", GATEWAY, [("ready", "/readyz", "ready")],
         [("operator_queue_hidden", "/internal/v1/rights-cases/recipient/consumer-one", "GET")]),
        ("lokos_adapter", ADAPTER, [("ready", "/readyz", "ready"),
                                   ("manifest", "/.well-known/gramstack-adapter", "manifest")],
         [("verification_hidden", "/internal/v1/subject-verifications", "POST"),
          ("retrieval_hidden", "/internal/v1/retrievals", "POST")]),
        ("sakhicredit", PRODUCT, [("ready", "/readyz", "product_ready")], []),
    ]
    with ThreadPoolExecutor(max_workers=len(services)) as executor:
        results = dict(executor.map(check, services))
    passed = all(check["passed"] is True for service in results.values() for check in service.values())
    print(json.dumps({"passed": passed, "services": results}, indent=2))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
