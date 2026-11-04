"""Check GRAMStack's private demo mTLS routes from a deployment operator host."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit


TARGETS = {
    "data": ("gateway.railway.internal", 8445),
    "adapter": ("lokos-adapter.railway.internal", 8443),
    "source": ("mock-lokos.railway.internal", 8444),
}


def local_targets(state):
    endpoints = json.loads((state / "endpoints.json").read_text())
    targets = {}
    for name in TARGETS:
        parsed = urlsplit(endpoints[name])
        if parsed.scheme != "https" or not parsed.port or parsed.netloc != f"127.0.0.1:{parsed.port}" or (
            parsed.path or parsed.query or parsed.fragment
        ):
            raise ValueError("Expected standalone HTTPS loopback endpoints")
        targets[name] = (parsed.hostname, parsed.port)
    return targets


def request(target, certs, identity, method, path, forward_port=None):
    host, port = target
    command = [
        "curl", "--disable", "--silent", "--noproxy", "*",
        "--connect-timeout", "3", "--max-time", "5", "--cacert", str(certs / "ca.pem"),
        "--request", method, "--header", "Content-Type: text/plain",
        "--output", "/dev/null", "--write-out", "%{http_code}",
    ]
    if forward_port is not None:
        command += ["--connect-to", f"{host}:{port}:127.0.0.1:{forward_port}"]
    if identity:
        command += ["--cert", str(certs / f"{identity}.pem"), "--key", str(certs / f"{identity}.key")]
    # The adapter probe deliberately sends no valid contract or JSON.
    result = subprocess.run(command + [f"https://{host}:{port}{path}"],
                            capture_output=True, text=True, timeout=7)
    if result.returncode == 0:
        return int(result.stdout)
    if result.returncode in (35, 56) and not identity:
        return "tls_rejected"
    return "unreachable"


def check(targets, certs, forwarded=None):
    registry_queue = "/internal/v1/rights-cases/registry/lokos"
    recipient_queue = "/internal/v1/rights-cases/recipient/consumer-one"
    verification = "/internal/v1/subject-verifications"
    probes = (
        ("registry_queue", "data", "lokos-rights", "GET", registry_queue, 200),
        ("recipient_queue", "data", "consumer-one", "GET", recipient_queue, 200),
        ("recipient_on_registry_queue", "data", "consumer-one", "GET", registry_queue, 404),
        ("adapter_on_registry_queue", "data", "lokos-adapter", "GET", registry_queue, 404),
        ("gateway_without_certificate", "data", None, "GET", registry_queue, "tls_rejected"),
        ("gateway_on_verification", "adapter", "gateway", "POST", verification, 400),
        ("recipient_on_verification", "adapter", "consumer-one", "POST", verification, 403),
        ("adapter_on_source", "source", "lokos-adapter", "GET", "/healthz", 200),
        ("gateway_on_source", "source", "gateway", "GET", "/healthz", 403),
    )
    results = {}
    for label, service, identity, method, path, expected in probes:
        observed = request(targets[service], certs, identity, method, path, (forwarded or {}).get(service))
        results[label] = {"observed": observed, "passed": observed == expected}
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--certs", type=Path, help="private operator directory for the Railway demo certificates")
    mode.add_argument("--local-state", type=Path, help="running standalone demo directory")
    parser.add_argument("--forwarded-ports", type=int, nargs=3, metavar=("GATEWAY", "ADAPTER", "SOURCE"),
                        help="local SSH forward ports, in gateway, adapter, source order")
    args = parser.parse_args()
    forwarded = None
    if args.forwarded_ports:
        if not args.certs or len(set(args.forwarded_ports)) != 3 or any(
            not 1 <= port <= 65535 for port in args.forwarded_ports
        ):
            parser.error("Use --certs and three distinct, valid loopback SSH forward ports")
        forwarded = dict(zip(TARGETS, args.forwarded_ports))
    try:
        targets, certs = (local_targets(args.local_state), args.local_state / "certs") if args.local_state else (
            TARGETS, args.certs
        )
        results = check(targets, certs, forwarded)
    except (OSError, KeyError, ValueError, subprocess.TimeoutExpired):
        parser.error("Check the demo endpoints and operator certificate files")
    passed = all(result["passed"] for result in results.values())
    print(json.dumps({"passed": passed, "checks": results}, indent=2))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
