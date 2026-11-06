"""Isolated loopback protocol fixture for all consumer SDKs. Never contacts a deployed service."""
import base64
from datetime import datetime, timedelta, timezone
import hashlib
import http.server
import json
from pathlib import Path
import secrets
import signal
import ssl
import sys
import threading
import time
from urllib.parse import parse_qs, unquote_plus, urlsplit

import jwt
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

directory = Path(sys.argv[1]).parent
directory.mkdir(mode=0o700, exist_ok=True)
key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "SDK isolated test CA")])
now = datetime.now(timezone.utc)
ca = (x509.CertificateBuilder().subject_name(subject).issuer_name(subject).public_key(key.public_key())
      .serial_number(1).not_valid_before(now - timedelta(minutes=1)).not_valid_after(now + timedelta(hours=1))
      .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True).sign(key, hashes.SHA256()))
(directory / "ca.pem").write_bytes(ca.public_bytes(serialization.Encoding.PEM))


def certificate(name, serial, usage):
    private = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    cert = (x509.CertificateBuilder().subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)]))
            .issuer_name(subject).public_key(private.public_key()).serial_number(serial)
            .not_valid_before(now - timedelta(minutes=1)).not_valid_after(now + timedelta(hours=1))
            .add_extension(x509.SubjectAlternativeName([x509.DNSName(name)]), critical=False)
            .add_extension(x509.ExtendedKeyUsage([usage]), critical=False).sign(key, hashes.SHA256()))
    cert_file = directory / f"{serial}.pem"
    key_file = directory / f"{serial}.key"
    cert_file.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    key_file.write_bytes(private.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    return cert_file, key_file


server_cert, server_key = certificate("gateway.sdk.test", 2, x509.oid.ExtendedKeyUsageOID.SERVER_AUTH)
client_cert, client_key = certificate("consumer-one", 3, x509.oid.ExtendedKeyUsageOID.CLIENT_AUTH)
jwt_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
jwk = json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(jwt_key.public_key()))
jwk.update(kid="sdk-key", use="sig", alg="RS256")
lock = threading.Lock()
state = {}
base = ""


def reset(mode):
    state.clear()
    state.update(mode=mode, tokens=0, creates=0, fetches=0, traps=0, exchanges=0, codes={}, fields=["membership_status"], status="AUTHENTICATION_PENDING", consumed=False)


reset("normal")


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def reply(self, status, value, *, contract=False):
        body = json.dumps(value, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        if contract:
            self.send_header("Gramstack-Contract-Version", "0.2.0" if state["mode"] == "wrong_version" else "0.3.0")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.handle_request()

    def do_POST(self):
        self.handle_request()

    def handle_request(self):
        length = int(self.headers.get("Content-Length", "0"))
        if length > 65536:
            return self.reply(400, {})
        body = self.rfile.read(length)
        path = urlsplit(self.path).path
        with lock:
            mode = state["mode"]
            if path == "/_fixture/reset":
                reset(json.loads(body).get("mode", "normal"))
                return self.reply(200, {})
            if path == "/_fixture/status":
                state["status"] = json.loads(body)["status"]
                return self.reply(200, {})
            if path == "/_fixture/counts":
                return self.reply(200, {key: state[key] for key in ("tokens", "creates", "fetches", "traps", "exchanges")})
            if path == "/_fixture/authorize":
                query = parse_qs(urlsplit(json.loads(body)["url"]).query)
                code = secrets.token_urlsafe(24)
                state["codes"][code] = query
                callback = query["redirect_uri"][0] + "?" + __import__("urllib.parse", fromlist=["urlencode"]).urlencode({"state": query["state"][0], "code": code})
                return self.reply(200, {"callback": callback})
            if path == "/_fixture/trap":
                state["traps"] += 1
                return self.reply(200, {})
            if path == "/.well-known/openid-configuration":
                return self.reply(200, {"issuer": base, "authorization_endpoint": base + "/authorize",
                    "token_endpoint": base + "/oauth2/token", "jwks_uri": base + "/jwks",
                    "response_types_supported": ["code"], "subject_types_supported": ["public"],
                    "id_token_signing_alg_values_supported": ["RS256"]})
            if path == "/jwks":
                return self.reply(200, {"keys": [jwk]})
            if path == "/oauth2/token":
                form = parse_qs(body.decode())
                authorization = self.headers.get("Authorization", "")
                try:
                    credentials = base64.b64decode(authorization.removeprefix("Basic ")).decode()
                    client, secret = [unquote_plus(value) for value in credentials.split(":", 1)]
                except Exception:
                    return self.reply(401, {})
                if secret != "fixture-secret":
                    return self.reply(401, {})
                if form.get("grant_type") == ["client_credentials"]:
                    state["tokens"] += 1
                    if client != "consumer-one":
                        return self.reply(401, {})
                    return self.reply(200, {"access_token": "fixture-access-token", "token_type": "bearer", "expires_in": 300, "scope": "gramstack.access"})
                state["exchanges"] += 1
                query = state["codes"].pop(form.get("code", [""])[0], None)
                if not query or client != query["client_id"][0] or form.get("redirect_uri") != query["redirect_uri"]:
                    return self.reply(400, {})
                verifier = form.get("code_verifier", [""])[0]
                challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
                if challenge != query["code_challenge"][0]:
                    return self.reply(400, {})
                claims = dict(iss=base, sub="app_fixture_subject", aud=client, iat=int(time.time()),
                              exp=int(time.time()) + 300, auth_time=int(time.time()), nonce=query["nonce"][0])
                if mode == "wrong_nonce":
                    claims["nonce"] = "wrong"
                if mode == "wrong_audience":
                    claims["aud"] = "other-client"
                if mode == "extra_audience":
                    claims["aud"] = [client, "other-client"]
                if mode == "wrong_issuer":
                    claims["iss"] = "https://untrusted.example"
                if mode == "expired":
                    claims["exp"] = int(time.time()) - 10
                if mode == "missing_auth_time":
                    claims.pop("auth_time")
                signing_key = key if mode == "invalid_signature" else jwt_key
                token = jwt.encode(claims, signing_key, algorithm="RS256", headers={"kid": "sdk-key"})
                return self.reply(200, {"access_token": "application-only-token", "token_type": "bearer", "expires_in": 300, "id_token": token})
            if mode == "redirect":
                self.send_response(302)
                self.send_header("Location", base + "/_fixture/trap")
                self.end_headers()
                return
            if mode == "unauthorized":
                return self.reply(401, {})
            if path.endswith("/data"):
                if not isinstance(self.connection, ssl.SSLSocket) or not self.connection.getpeercert():
                    return self.reply(403, {})
                if self.headers.get("Authorization") != "Bearer fixture.grant.jwt" or state["consumed"]:
                    return self.reply(409, {})
                state["consumed"] = True
                state["fetches"] += 1
                if mode == "disconnect":
                    self.connection.shutdown(2)
                    self.connection.close()
                    return
                data = {"membership_status": "active"}
                if "savings_summary" in state["fields"]:
                    data["savings_summary"] = {"amount": 12500.75, "currency": "INR"}
                if mode == "extra_field":
                    data["unapproved_field"] = True
                return self.reply(200, {"request_id": "request-1", "data": data, "receipt_id": "receipt-1"}, contract=True)
            if self.headers.get("Authorization") != "Bearer fixture-access-token":
                return self.reply(401, {})
            if path == "/v1/access-requests":
                state["creates"] += 1
                state["fields"] = json.loads(body)["fields"]
                value = {"request_id": "request-1", "status": "AUTHENTICATION_PENDING",
                         "status_url": base + "/v1/access-requests/request-1",
                         "interaction_url": base + "/interactions/request-1", "expires_at": "2090-01-01T00:00:00Z"}
                if mode == "foreign_locator":
                    value["interaction_url"] = "https://untrusted.example/interactions/request-1"
                return self.reply(202, value, contract=True)
            if path.endswith("/grant"):
                return self.reply(200, {"grant": "fixture.grant.jwt", "expires_at": "2090-01-01T00:00:00Z", "token_type": "Bearer"}, contract=True)
            if path.endswith("/cancel"):
                state["status"] = "CANCELLED"
            value = {"request_id": "request-1", "status": state["status"], "version": 2,
                     "next_action": "review_consent", "expires_at": "2090-01-01T00:00:00Z"}
            if mode == "duplicate_json":
                raw = b'{"request_id":"request-1","request_id":"other","status":"CONSENTED","version":2,"next_action":"none","expires_at":"2090-01-01T00:00:00Z"}'
                self.send_response(200)
                self.send_header("Gramstack-Contract-Version", "0.3.0")
                self.end_headers()
                self.wfile.write(raw)
                return
            return self.reply(200, value, contract=True)


public = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
private = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.minimum_version = ssl.TLSVersion.TLSv1_3
context.load_cert_chain(server_cert, server_key)
context.load_verify_locations(directory / "ca.pem")
context.verify_mode = ssl.CERT_REQUIRED
private.socket = context.wrap_socket(private.socket, server_side=True)
base = f"http://127.0.0.1:{public.server_port}"
config = dict(public_origin=base, token_url=base + "/oauth2/token", client_id="consumer-one", client_secret="fixture-secret",
              data_origin=f"https://127.0.0.1:{private.server_port}", data_server_name="gateway.sdk.test",
              ca_file=str(directory / "ca.pem"), certificate_file=str(client_cert), key_file=str(client_key), allow_loopback=True,
              issuer=base, redirect_url="http://127.0.0.1:18773/callback")
threading.Thread(target=private.serve_forever, daemon=True).start()
threading.Thread(target=public.serve_forever, daemon=True).start()
Path(sys.argv[1]).write_text(json.dumps(config))
signal.pause()
