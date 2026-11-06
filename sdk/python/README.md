# GRAMStack Python client

```sh
python -m pip install https://github.com/theflywheel/GRAMStack/releases/download/client-sdks-v0.1.0/gramstack_client-0.1.0-py3-none-any.whl
```

Requires Python 3.10+. Consumer OAuth and protected retrieval run on the backend.

```python
from gramstack import Client, Config

client = Client(Config(
    public_origin=gateway_origin,
    token_url=token_url,
    client_id=client_id,
    client_secret=client_secret,
    data_origin=private_data_origin,
    data_server_name=data_tls_name,
    ca_file=ca_file,
    certificate_file=consumer_certificate,
    key_file=consumer_private_key,
))
request = {
    "registry": "lokos",
    "resource": "shg.membership",
    "subject_hint": {"type": "member_reference", "value": member_reference},
    "fields": ["membership_status"],
    "purpose": "credit_eligibility",
    "access": {"mode": "single_use", "valid_for_seconds": 300},
    "recipient_terms": {"retention_policy_id": "terms-v1"},
}
accepted = client.create("application-482", request)
# Return accepted.interaction_url to the browser and wait for explicit consent.
status = client.status(accepted.request_id)
if status.status == "CONSENTED":
    grant = client.issue_grant(accepted.request_id, "application-482-grant")
    result = client.retrieve(grant, request["fields"])
```

`cancel` closes unadmitted requests. `wait_for_consent` accepts a timeout and
polls metadata only. `history_url` returns the person's protected history page.
Methods are synchronous; use your framework's thread pool in async handlers.

The grant is an opaque one-attempt handle. No retrieval redirect or retry is
performed. Keep durable attempt state in the product. Fractional JSON numbers
are preserved as `Decimal`.

## Standalone sign-in

```python
from gramstack import AuthClient, AuthConfig

auth = AuthClient(AuthConfig(
    issuer=hydra_issuer,
    client_id=application_client_id,
    client_secret=application_client_secret,
    redirect_url=exact_registered_callback,
), login_store)
authorization_url = auth.begin(server_owned_browser_binding)
identity = auth.complete(same_browser_binding, exact_callback_url)
```

Keep the browser binding in a Secure, HttpOnly, SameSite cookie. The callback
must use that cookie's binding, not a binding supplied in query/body fields.
The store implements `save_login` and `take_login` with encrypted, shared TTL
state and atomic browser-bound consumption. The SDK validates PKCE, nonce, state,
issuer, audience, signature and fresh authentication. The product then creates
its own authenticated session. `MemoryLoginStore` is explicitly for development
in one process.

See the [cross-language integration boundaries](../README.md) and local playground
notes under `sdk/examples/`.
