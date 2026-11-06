"""Server-side GRAMStack client. Every retrieval is one attempt over fresh mTLS."""
from __future__ import annotations

import base64
import http.client
import ipaddress
import json
import re
import socket
import ssl
import threading
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from decimal import Decimal
from typing import Any, TypedDict
from urllib.parse import quote_plus, urlencode, urlsplit

VERSION = "0.3.0"
ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
STATES = frozenset(("AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED", "REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED", "COMPLETED", "DELIVERY_UNKNOWN", "REJECTED", "DENIED", "WITHDRAWN", "EXPIRED", "FAILED", "CANCELLED"))
TERMINAL = STATES - {"AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED", "REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED"}


class GramstackError(Exception):
    """Safe SDK error. Tokens, source records and credentials are excluded."""


class TransportError(GramstackError):
    pass


class AlreadyAttempted(GramstackError):
    pass


class RemoteError(GramstackError):
    def __init__(self, status: int):
        self.status = status
        super().__init__(f"GRAMStack operation failed (HTTP {status})")


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise GramstackError("Duplicate JSON property")
        result[key] = value
    return result


def parse_json(raw: bytes) -> Any:
    try:
        return json.loads(raw, object_pairs_hook=_pairs, parse_float=Decimal,
                          parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    except (UnicodeError, ValueError) as error:
        raise GramstackError("Invalid GRAMStack JSON response") from None


def shape(value: Any, required: set[str], optional: set[str] = frozenset()) -> dict:
    if not isinstance(value, dict) or not required <= value.keys() or value.keys() - required - optional:
        raise GramstackError("Invalid GRAMStack response shape")
    return value


def timestamp(value: Any) -> datetime:
    if not isinstance(value, str):
        raise GramstackError("Invalid timestamp")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        raise GramstackError("Invalid timestamp") from None
    if parsed.tzinfo is None:
        raise GramstackError("Timestamp must include timezone")
    return parsed


def identifier(value: Any) -> str:
    if not isinstance(value, str) or not ID.fullmatch(value):
        raise GramstackError("Invalid GRAMStack identifier")
    return value


def fields_valid(fields: Any) -> list[str]:
    if not isinstance(fields, (list, tuple)) or not 1 <= len(fields) <= 128:
        raise GramstackError("One or more explicit fields are required")
    for value in fields:
        identifier(value)
    if len(set(fields)) != len(fields):
        raise GramstackError("Fields must be unique")
    return list(fields)


def trusted_url(value: str, loopback: bool = False, origin: bool = False):
    if not isinstance(value, str):
        raise GramstackError("A pinned HTTPS endpoint is required")
    try:
        parsed = urlsplit(value)
        _ = parsed.port
        if not parsed.hostname or parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment or "?" in value or "#" in value or (origin and parsed.path):
            raise ValueError()
        if parsed.scheme != "https":
            if not loopback or parsed.scheme != "http" or not ipaddress.ip_address(parsed.hostname).is_loopback:
                raise ValueError()
        return parsed
    except (ValueError, TypeError):
        raise GramstackError("A pinned HTTPS endpoint is required") from None


class _TLSConnection(http.client.HTTPSConnection):
    def __init__(self, host, port, *, context, timeout, server_name=None):
        super().__init__(host, port, context=context, timeout=timeout)
        self._server_name = server_name or host

    def connect(self):
        raw = socket.create_connection((self.host, self.port), timeout=self.timeout)
        try:
            self.sock = self._context.wrap_socket(raw, server_hostname=self._server_name)
        except Exception:
            raw.close()
            raise


def ssl_context(ca_file=None, *, private=False, certificate_file=None, key_file=None):
    context = ssl.create_default_context(cafile=ca_file if private else None)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    if ca_file and not private:
        context.load_verify_locations(cafile=ca_file)
    if certificate_file:
        context.load_cert_chain(certificate_file, key_file)
    return context


def wire(url, method, headers, body, *, context=None, timeout=10, server_name=None):
    parsed = trusted_url(url, loopback=True)
    connection = (_TLSConnection(parsed.hostname, parsed.port or 443, context=context, timeout=timeout, server_name=server_name)
                  if parsed.scheme == "https" else http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=timeout))
    try:
        # Fresh connections and http.client make exactly one request and never follow redirects.
        connection.request(method, parsed.path or "/", body=body, headers=headers)
        response = connection.getresponse()
        raw = response.read(65537)
        if len(raw) > 65536:
            raise TransportError("GRAMStack response exceeded its limit")
        return response.status, dict((key.lower(), value) for key, value in response.getheaders()), raw
    except (OSError, http.client.HTTPException, ValueError):
        raise TransportError("GRAMStack transport did not complete") from None
    finally:
        connection.close()


class SubjectHint(TypedDict):
    type: str
    value: str


class Access(TypedDict):
    mode: str
    valid_for_seconds: int


class RecipientTerms(TypedDict):
    retention_policy_id: str


class AccessRequest(TypedDict):
    registry: str
    resource: str
    subject_hint: SubjectHint
    fields: list[str]
    purpose: str
    access: Access
    recipient_terms: RecipientTerms


@dataclass(frozen=True)
class Config:
    public_origin: str
    token_url: str
    client_id: str
    client_secret: str = field(repr=False)
    data_origin: str = ""
    data_server_name: str = ""
    ca_file: str | None = None
    certificate_file: str | None = None
    key_file: str | None = field(default=None, repr=False)
    allow_loopback: bool = False


@dataclass(frozen=True)
class Accepted:
    request_id: str
    status: str
    status_url: str
    interaction_url: str
    expires_at: datetime


@dataclass(frozen=True)
class Status:
    request_id: str
    status: str
    version: int
    next_action: str
    expires_at: datetime


@dataclass(frozen=True, repr=False)
class Grant:
    expires_at: datetime
    _token: str = field(repr=False)
    _request_id: str
    _client: Any = field(repr=False)
    _attempted: threading.Event = field(default_factory=threading.Event, repr=False)
    _lock: Any = field(default_factory=threading.Lock, repr=False)

    def __repr__(self):
        return "Grant(<redacted>)"


@dataclass(frozen=True)
class Result:
    request_id: str
    data: dict[str, Any]
    receipt_id: str


class Client:
    def __init__(self, config: Config):
        trusted_url(config.public_origin, config.allow_loopback, origin=True)
        trusted_url(config.token_url, config.allow_loopback)
        identifier(config.client_id)
        if not isinstance(config.client_secret, str) or not config.client_secret:
            raise GramstackError("Consumer credentials required")
        self._config = config
        self._public_context = ssl_context(config.ca_file)
        self._private_context = None
        if config.data_origin:
            trusted_url(config.data_origin, origin=True)
            if not all((config.data_server_name, config.ca_file, config.certificate_file, config.key_file)):
                raise GramstackError("Retrieval needs its pinned mTLS configuration")
            self._private_context = ssl_context(config.ca_file, private=True, certificate_file=config.certificate_file, key_file=config.key_file)
        self._token = ""
        self._token_until = 0
        self._lock = threading.Lock()

    def _access_token(self):
        with self._lock:
            if self._token and time.monotonic() < self._token_until - 10:
                return self._token
            credentials = f"{quote_plus(self._config.client_id)}:{quote_plus(self._config.client_secret)}"
            authorization = base64.b64encode(credentials.encode()).decode()
            status, _, raw = wire(self._config.token_url, "POST",
                {"Authorization": "Basic " + authorization, "Content-Type": "application/x-www-form-urlencoded"},
                urlencode({"grant_type": "client_credentials", "scope": "gramstack.access", "audience": "gramstack"}).encode(), context=self._public_context)
            if status != 200:
                raise RemoteError(status)
            value = parse_json(raw)
            if not isinstance(value, dict) or not isinstance(value.get("access_token"), str) or not 1 <= len(value["access_token"]) <= 32768 or str(value.get("token_type")).lower() != "bearer" or type(value.get("expires_in")) is not int or not 1 <= value["expires_in"] <= 86400:
                raise GramstackError("Invalid OAuth token response")
            self._token = value["access_token"]
            self._token_until = time.monotonic() + value["expires_in"]
            return self._token

    def _call(self, method, path, key=None, body=None):
        token = self._access_token()
        headers = {"Authorization": "Bearer " + token, "Gramstack-Contract-Version": VERSION}
        if key:
            headers["Idempotency-Key"] = identifier(key)
        if body is not None:
            headers["Content-Type"] = "application/json"
        status, response_headers, raw = wire(self._config.public_origin + path, method, headers,
             json.dumps(body, allow_nan=False, separators=(",", ":")).encode() if body is not None else None,
             context=self._public_context)
        if not 200 <= status < 300:
            if status == 401:
                with self._lock:
                    if self._token == token:
                        self._token = ""
            raise RemoteError(status)
        if response_headers.get("gramstack-contract-version") != VERSION:
            raise GramstackError("Unsupported GRAMStack contract version")
        return parse_json(raw)

    def create(self, idempotency_key: str, request: AccessRequest) -> Accepted:
        shape(request, {"registry", "resource", "subject_hint", "fields", "purpose", "access", "recipient_terms"})
        for key in ("registry", "resource", "purpose"):
            identifier(request[key])
        shape(request["subject_hint"], {"type", "value"})
        identifier(request["subject_hint"]["type"])
        hint = request["subject_hint"]["value"]
        if not isinstance(hint, str) or not 1 <= len(hint) <= 1024:
            raise GramstackError("Invalid subject hint")
        fields_valid(request["fields"])
        shape(request["access"], {"mode", "valid_for_seconds"})
        validity = request["access"]["valid_for_seconds"]
        if request["access"]["mode"] != "single_use" or type(validity) is not int or not 1 <= validity <= 300:
            raise GramstackError("Unsupported access mode")
        shape(request["recipient_terms"], {"retention_policy_id"})
        identifier(request["recipient_terms"]["retention_policy_id"])
        value = self._call("POST", "/v1/access-requests", identifier(idempotency_key), request)
        shape(value, {"request_id", "status", "status_url", "interaction_url", "expires_at"})
        identifier(value["request_id"])
        suffix = "/v1/access-requests/" + value["request_id"]
        if value["status_url"] != self._config.public_origin + suffix or value["interaction_url"] != self._config.public_origin + "/interactions/" + value["request_id"] or value["status"] != "AUTHENTICATION_PENDING":
            raise GramstackError("Request locator or initial status mismatch")
        return Accepted(**(value | {"expires_at": timestamp(value["expires_at"])}))

    def _status(self, request_id, cancel=False):
        identifier(request_id)
        value = self._call("POST" if cancel else "GET", "/v1/access-requests/" + request_id + ("/cancel" if cancel else ""))
        shape(value, {"request_id", "status", "version", "next_action", "expires_at"})
        if value["request_id"] != request_id or value["status"] not in STATES or type(value["version"]) is not int or value["version"] < 1 or not isinstance(value["next_action"], str):
            raise GramstackError("Invalid request status")
        return Status(**(value | {"expires_at": timestamp(value["expires_at"])}))

    def status(self, request_id: str) -> Status:
        return self._status(request_id)

    def cancel(self, request_id: str) -> Status:
        return self._status(request_id, cancel=True)

    def issue_grant(self, request_id: str, idempotency_key: str) -> Grant:
        identifier(request_id)
        value = self._call("POST", "/v1/access-requests/" + request_id + "/grant", identifier(idempotency_key))
        shape(value, {"grant", "expires_at", "token_type"})
        until = timestamp(value["expires_at"])
        if not isinstance(value["grant"], str) or not 1 <= len(value["grant"]) <= 32768 or value["token_type"] != "Bearer" or until <= datetime.now(timezone.utc):
            raise GramstackError("Invalid retrieval grant")
        return Grant(until, value["grant"], request_id, self)

    def retrieve(self, grant: Grant, fields: list[str]) -> Result:
        selected = fields_valid(fields)
        if not isinstance(grant, Grant) or grant._client is not self or self._private_context is None or grant.expires_at <= datetime.now(timezone.utc):
            raise GramstackError("A fresh grant and configured mTLS client are required")
        with grant._lock:
            if grant._attempted.is_set():
                raise AlreadyAttempted("Retrieval was already attempted; inspect request status")
            grant._attempted.set()
        status, headers, raw = wire(self._config.data_origin + "/v1/access-requests/" + grant._request_id + "/data",
             "GET", {"Authorization": "Bearer " + grant._token}, None, context=self._private_context, timeout=30,
             server_name=self._config.data_server_name)
        if status != 200:
            raise RemoteError(status)
        if headers.get("gramstack-contract-version") != VERSION:
            raise GramstackError("Unsupported GRAMStack contract version")
        value = shape(parse_json(raw), {"request_id", "data", "receipt_id"})
        if value["request_id"] != grant._request_id or not isinstance(value["data"], dict) or set(value["data"]) != set(selected):
            raise GramstackError("Disclosure did not match its request and exact fields")
        identifier(value["receipt_id"])
        return Result(**value)

    def history_url(self, registry: str) -> str:
        return self._config.public_origin + "/consent-history/" + identifier(registry)

    def wait_for_consent(self, request_id: str, *, timeout: float, interval: float = 1) -> Status:
        if timeout <= 0 or interval < 1:
            raise GramstackError("A positive timeout and polling interval >= 1 are required")
        deadline = time.monotonic() + timeout
        while True:
            value = self.status(request_id)
            if value.status in TERMINAL or value.status in {"CONSENTED", "AUTHORIZED"}:
                return value
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("Consent wait timed out")
            time.sleep(min(interval, remaining))
