"""Prepare and register the two demo clients on the private Hydra admin listener."""
import argparse
import json
import os
from pathlib import Path
import re
import secrets
import ssl
from urllib.error import HTTPError
from urllib.request import (
    HTTPRedirectHandler, HTTPSHandler, ProxyHandler, Request, build_opener,
)

ADMIN = "https://hydra-admin.railway.internal:4445"
CALLBACK = "https://gramstack-gateway-b9b69819.up.railway.app/auth/callback"


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, target):
        return None


def prepare(path):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        json.dump({name: secrets.token_hex(32) for name in ("consumer-one", "gramstack-portal")}, output)


def register(path, ca_file, local_port):
    if path.is_symlink() or not path.is_file() or path.stat().st_mode & 0o077:
        raise ValueError("Use a private regular client secret file")
    if path.stat().st_size > 1024:
        raise ValueError("Client secret file is too large")
    values = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(values, dict) or set(values) != {"consumer-one", "gramstack-portal"}:
        raise ValueError("Expected the two demo client secrets")
    if not all(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) for value in values.values()):
        raise ValueError("Invalid demo client secret format")
    admin = f"https://127.0.0.1:{local_port}" if local_port else ADMIN
    context = ssl.create_default_context(cafile=ca_file)
    opener = build_opener(ProxyHandler({}), HTTPSHandler(context=context), NoRedirect())
    clients = [
        dict(client_id="consumer-one", grant_types=["client_credentials"], scope="gramstack.access",
             audience=["gramstack"], token_endpoint_auth_method="client_secret_basic"),
        dict(client_id="gramstack-portal", grant_types=["authorization_code"], response_types=["code"],
             scope="openid", redirect_uris=[CALLBACK], token_endpoint_auth_method="client_secret_basic"),
    ]
    for client in clients:
        client_id = client["client_id"]
        try:
            with opener.open(admin + "/admin/clients/" + client_id, timeout=8):
                raise ValueError(f"Client {client_id} already exists. Resolve it before provisioning.")
        except HTTPError as error:
            if error.code != 404:
                raise ValueError(f"Admin lookup failed with HTTP {error.code}") from None
    for client in clients:
        client_id = client["client_id"]
        body = json.dumps(client | {"client_secret": values[client_id]}).encode()
        try:
            request = Request(admin + "/admin/clients", body, {"Content-Type": "application/json"})
            with opener.open(request, timeout=8) as response:
                if response.status != 201:
                    raise ValueError(f"Unexpected status for {client_id}")
        except HTTPError as error:
            raise ValueError(f"Client creation failed with HTTP {error.code} for {client_id}") from None
        print(f"Registered {client_id}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    generate = sub.add_parser("prepare")
    generate.add_argument("secrets", type=Path)
    create = sub.add_parser("register")
    create.add_argument("secrets", type=Path)
    create.add_argument("--ca", type=Path, required=True)
    create.add_argument("--local-admin-port", type=int,
                        help="use literal loopback HTTPS for an isolated test")
    args = parser.parse_args()
    try:
        if args.command == "prepare":
            prepare(args.secrets)
            print("Prepared private Hydra client settings.")
        else:
            if args.local_admin_port is not None and not 1 <= args.local_admin_port <= 65535:
                parser.error("Invalid local admin port")
            register(args.secrets, args.ca, args.local_admin_port)
    except (OSError, ValueError) as error:
        parser.exit(1, f"Hydra provisioning stopped ({type(error).__name__}). "
                       "Keep the secret file and inspect the private admin state before retrying.\n")


if __name__ == "__main__":
    main()
