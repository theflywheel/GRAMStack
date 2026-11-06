import json
import os
from pathlib import Path
import secrets
from concurrent.futures import ThreadPoolExecutor
from urllib.request import Request, urlopen

import pytest
from gramstack import (AlreadyAttempted, AuthenticationError, AuthClient, AuthConfig, Client, Config,
                      GramstackError, MemoryLoginStore, RemoteError, TransportError)

CONFIG = json.loads(Path(os.environ["GRAMSTACK_SDK_FIXTURE"]).read_text())
BODY = dict(registry="lokos", resource="shg.membership", subject_hint=dict(type="member_reference", value="member-12345"),
            fields=["membership_status"], purpose="credit_eligibility", access=dict(mode="single_use", valid_for_seconds=300),
            recipient_terms=dict(retention_policy_id="terms-v1"))


def control(path, body=None):
    request = Request(CONFIG["public_origin"] + "/_fixture/" + path,
                      data=json.dumps(body).encode() if body is not None else None,
                      headers={"Content-Type": "application/json"})
    with urlopen(request) as response:
        return json.load(response)


def client():
    return Client(Config(**{key: value for key, value in CONFIG.items() if key not in {"issuer", "redirect_url"}}))


def auth():
    return AuthClient(AuthConfig(CONFIG["issuer"], "app-one", CONFIG["redirect_url"], "fixture-secret", allow_loopback=True), MemoryLoginStore())


def test_lifecycle_token_cache_and_exact_projection():
    control("reset", {})
    sdk = client()
    accepted = sdk.create("test-create", BODY)
    assert accepted.interaction_url.startswith(CONFIG["public_origin"])
    assert sdk.status(accepted.request_id).status == "AUTHENTICATION_PENDING"
    grant = sdk.issue_grant(accepted.request_id, "test-grant")
    assert "fixture.grant.jwt" not in repr(grant)
    result = sdk.retrieve(grant, BODY["fields"])
    assert result.data == {"membership_status": "active"} and result.receipt_id == "receipt-1"
    with pytest.raises(AlreadyAttempted):
        sdk.retrieve(grant, BODY["fields"])
    assert control("counts")["fetches"] == 1
    assert control("counts")["tokens"] == 1
    assert sdk.cancel(accepted.request_id).status == "CANCELLED"


@pytest.mark.parametrize("mode", ["redirect", "wrong_version", "duplicate_json"])
def test_fail_closed_metadata(mode):
    control("reset", {"mode": mode})
    with pytest.raises(GramstackError):
        client().status("request-1")
    assert control("counts")["traps"] == 0


def test_reject_foreign_consent_locator():
    control("reset", {"mode": "foreign_locator"})
    with pytest.raises(GramstackError):
        client().create("test-create", BODY)


@pytest.mark.parametrize("mode", ["extra_field", "disconnect"])
def test_retrieval_failure_is_never_retried(mode):
    control("reset", {"mode": mode})
    sdk = client()
    sdk.create("test-create", BODY)
    grant = sdk.issue_grant("request-1", "test-grant")
    with pytest.raises(GramstackError):
        sdk.retrieve(grant, BODY["fields"])
    with pytest.raises(AlreadyAttempted):
        sdk.retrieve(grant, BODY["fields"])
    assert control("counts")["fetches"] == 1


def test_concurrent_calls_share_one_attempt():
    control("reset", {})
    sdk = client()
    sdk.create("test-create", BODY)
    grant = sdk.issue_grant("request-1", "test-grant")
    def retrieve(_):
        try:
            return sdk.retrieve(grant, BODY["fields"]).receipt_id
        except AlreadyAttempted:
            return "blocked"
    with ThreadPoolExecutor(2) as executor:
        assert sorted(executor.map(retrieve, range(2))) == ["blocked", "receipt-1"]
    assert control("counts")["fetches"] == 1


def test_revoked_token_requires_explicit_retry():
    control("reset", {"mode": "unauthorized"})
    sdk = client()
    with pytest.raises(RemoteError):
        sdk.status("request-1")
    assert control("counts")["tokens"] == 1
    with pytest.raises(RemoteError):
        sdk.status("request-1")
    assert control("counts")["tokens"] == 2


def test_sign_in_browser_binding_and_single_use_callback():
    control("reset", {})
    sdk = auth()
    binding = secrets.token_urlsafe(32)
    callback = control("authorize", {"url": sdk.begin(binding)})["callback"]
    with pytest.raises(AuthenticationError):
        sdk.complete(secrets.token_urlsafe(32), callback)
    identity = sdk.complete(binding, callback)
    assert identity.subject == "app_fixture_subject"
    with pytest.raises(AuthenticationError):
        sdk.complete(binding, callback)
    assert control("counts")["exchanges"] == 1


@pytest.mark.parametrize("mode", ["wrong_nonce", "wrong_audience", "extra_audience", "wrong_issuer", "expired", "invalid_signature", "missing_auth_time"])
def test_invalid_identity_tokens_are_rejected(mode):
    control("reset", {"mode": mode})
    sdk = auth()
    binding = secrets.token_urlsafe(32)
    callback = control("authorize", {"url": sdk.begin(binding)})["callback"]
    with pytest.raises(GramstackError):
        sdk.complete(binding, callback)
