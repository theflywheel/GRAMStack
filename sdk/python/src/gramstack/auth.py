"""Standalone OIDC sign-in. Application sessions stay with the calling product."""
from __future__ import annotations

import base64
import hashlib
import hmac
import secrets
import threading
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Protocol
from urllib.parse import parse_qs, quote_plus, urlencode, urlsplit

import jwt

from .client import GramstackError, RemoteError, identifier, parse_json, ssl_context, trusted_url, wire


class AuthenticationError(GramstackError):
    pass


@dataclass(frozen=True)
class AuthConfig:
    issuer: str
    client_id: str
    redirect_url: str
    client_secret: str = field(default="", repr=False)
    ca_file: str | None = None
    allow_loopback: bool = False


@dataclass(frozen=True)
class LoginTransaction:
    state: str
    nonce: str
    verifier: str = field(repr=False)
    started_at: float
    expires_at: float


@dataclass(frozen=True)
class Identity:
    issuer: str
    subject: str
    authenticated_at: datetime
    expires_at: datetime


class LoginStore(Protocol):
    """Store encrypted TTL state and atomically consume only a matching browser binding."""
    def save_login(self, state: str, binding: str, transaction: LoginTransaction) -> None: ...
    def take_login(self, state: str, binding: str) -> LoginTransaction: ...


class MemoryLoginStore:
    """Explicit single-process development store. Use shared persistent state for production replicas."""
    def __init__(self):
        self._lock = threading.Lock()
        self._values = {}

    def save_login(self, state, binding, transaction):
        with self._lock:
            self._values = {key: value for key, value in self._values.items() if value.expires_at > time.time()}
            if len(self._values) >= 4096 or (state, binding) in self._values:
                raise AuthenticationError("Login state unavailable")
            self._values[(state, binding)] = transaction

    def take_login(self, state, binding):
        with self._lock:
            transaction = self._values.pop((state, binding), None)
        if transaction is None or transaction.expires_at <= time.time():
            raise AuthenticationError("Login state unavailable")
        return transaction


def _b64(value):
    return base64.urlsafe_b64encode(value).rstrip(b"=").decode()


class AuthClient:
    def __init__(self, config: AuthConfig, store: LoginStore):
        trusted_url(config.issuer, config.allow_loopback)
        trusted_url(config.redirect_url, config.allow_loopback)
        identifier(config.client_id)
        self._config, self._store = config, store
        self._context = ssl_context(config.ca_file)
        status, _, raw = wire(config.issuer.rstrip("/") + "/.well-known/openid-configuration", "GET", {}, None, context=self._context)
        if status != 200:
            raise AuthenticationError("OIDC discovery unavailable")
        value = parse_json(raw)
        if not isinstance(value, dict) or value.get("issuer") != config.issuer:
            raise AuthenticationError("Issuer mismatch")
        for key in ("authorization_endpoint", "token_endpoint", "jwks_uri"):
            endpoint = trusted_url(value.get(key, ""), config.allow_loopback)
            issuer = urlsplit(config.issuer)
            if (endpoint.scheme, endpoint.netloc) != (issuer.scheme, issuer.netloc):
                raise AuthenticationError("Untrusted OIDC endpoint")
        self._metadata = value

    def begin(self, browser_binding: str) -> str:
        if not isinstance(browser_binding, str) or not 26 <= len(browser_binding) <= 256:
            raise AuthenticationError("A server-owned browser binding is required")
        now = time.time()
        transaction = LoginTransaction(secrets.token_urlsafe(24), secrets.token_urlsafe(24), secrets.token_urlsafe(48), now, now + 300)
        self._store.save_login(transaction.state, browser_binding, transaction)
        query = urlencode({"client_id": self._config.client_id, "redirect_uri": self._config.redirect_url,
            "response_type": "code", "scope": "openid", "state": transaction.state, "nonce": transaction.nonce,
            "code_challenge": _b64(hashlib.sha256(transaction.verifier.encode()).digest()),
            "code_challenge_method": "S256", "prompt": "login", "max_age": "0"})
        return self._metadata["authorization_endpoint"] + "?" + query

    def complete(self, browser_binding: str, callback_url: str) -> Identity:
        actual, expected = urlsplit(callback_url), urlsplit(self._config.redirect_url)
        if (actual.scheme, actual.netloc, actual.path) != (expected.scheme, expected.netloc, expected.path) or actual.fragment:
            raise AuthenticationError("Callback mismatch")
        query = parse_qs(actual.query, keep_blank_values=True)
        if any(len(query.get(key, [])) != 1 for key in ("state", "code")) or query.get("error") or not 26 <= len(query["state"][0]) <= 256 or not 1 <= len(query["code"][0]) <= 8192:
            raise AuthenticationError("Invalid callback")
        transaction = self._store.take_login(query["state"][0], browser_binding)
        if transaction.state != query["state"][0] or transaction.expires_at <= time.time():
            raise AuthenticationError("Expired or mismatched login state")
        body = {"grant_type": "authorization_code", "code": query["code"][0], "redirect_uri": self._config.redirect_url, "code_verifier": transaction.verifier}
        headers = {"Content-Type": "application/x-www-form-urlencoded"}
        if self._config.client_secret:
            credentials = f"{quote_plus(self._config.client_id)}:{quote_plus(self._config.client_secret)}"
            headers["Authorization"] = "Basic " + base64.b64encode(credentials.encode()).decode()
        else:
            body["client_id"] = self._config.client_id
        status, _, raw = wire(self._metadata["token_endpoint"], "POST", headers, urlencode(body).encode(), context=self._context)
        if status != 200:
            raise AuthenticationError("Authorization code exchange failed")
        response = parse_json(raw)
        token = response.get("id_token", "") if isinstance(response, dict) else ""
        if not isinstance(token, str) or not 1 <= len(token) <= 32768:
            raise AuthenticationError("ID token unavailable")
        try:
            encoded_header, encoded_payload, _ = token.split(".")
            header = parse_json(base64.urlsafe_b64decode(encoded_header + "=" * (-len(encoded_header) % 4)))
            parse_json(base64.urlsafe_b64decode(encoded_payload + "=" * (-len(encoded_payload) % 4)))
            if not isinstance(header, dict) or header.get("alg") not in {"RS256", "ES256"}:
                raise ValueError()
            status, _, raw = wire(self._metadata["jwks_uri"], "GET", {}, None, context=self._context)
            keyset = parse_json(raw) if status == 200 else {}
            keys = keyset.get("keys", [])
            if not 1 <= len(keys) <= 16 or len({key.get("kid") for key in keys}) != len(keys):
                raise ValueError()
            matching = [key for key in keys if key.get("kid") == header.get("kid") and key.get("use", "sig") == "sig"]
            if len(matching) != 1 or matching[0].get("alg", header["alg"]) != header["alg"]:
                raise ValueError()
            key = jwt.PyJWK.from_dict(matching[0], algorithm=header["alg"]).key
            claims = jwt.decode(token, key, algorithms=["RS256", "ES256"], issuer=self._config.issuer,
                audience=self._config.client_id, options={"require": ["iss", "sub", "aud", "exp", "iat", "nonce", "auth_time"], "verify_iat": False, "verify_nbf": False})
            audience = claims["aud"]
            if audience != self._config.client_id and audience != [self._config.client_id]:
                raise ValueError()
            now = time.time()
            if not isinstance(claims["sub"], str) or not 1 <= len(claims["sub"]) <= 1024 or not hmac.compare_digest(claims["nonce"], transaction.nonce) or claims.get("azp", self._config.client_id) != self._config.client_id:
                raise ValueError()
            if any(type(claims.get(key)) is not int for key in ("exp", "iat", "auth_time")) or claims["iat"] > now + 30 or claims.get("nbf", 0) > now + 30 or not int(transaction.started_at) <= claims["auth_time"] <= now + 30 or now - claims["auth_time"] > 300:
                raise ValueError()
            if "at_hash" in claims:
                digest = _b64(hashlib.sha256(response.get("access_token", "").encode()).digest()[:16])
                if not hmac.compare_digest(claims["at_hash"], digest):
                    raise ValueError()
            return Identity(self._config.issuer, claims["sub"], datetime.fromtimestamp(claims["auth_time"], timezone.utc), datetime.fromtimestamp(claims["exp"], timezone.utc))
        except (ValueError, TypeError, KeyError, jwt.PyJWTError):
            raise AuthenticationError("ID token verification failed") from None
