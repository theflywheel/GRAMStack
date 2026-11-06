# Python SDK playground validation

The external [didi-ka-playground](https://github.com/karana100010/didi-ka-playground)
was checked out locally from `main` at
`5382853f5cbb3d0b3e511afa6976bdfc5bbd7ecd`.
No commit, push or pull request was made to that repository.

A local connector implements its existing `Connector.tables/check/rows` contract
using the Python SDK. It creates an explicit GRAMStack request, surfaces the
portal handoff URL, checks consent metadata, and retrieves only the approved
membership fields. The product's natural-language consent ledger does not grant
GRAMStack disclosure authority. Scope changes require cancellation and a new
request with explicit approval.

Validation used the shared loopback TLS/OIDC conformance server, fictional records,
a temporary SQLite database, a fake LLM and disabled tracing. No production LLM,
Bhashini or registry credentials were used.

```sh
python -m pip install -e /path/to/GRAMStack/sdk/python
python -m pip install -r requirements-dev.txt
# With the local connector/tests and GRAMSTACK_SDK_FIXTURE configured:
PLAYGROUND_FAKE_LLM=1 python -m pytest -q \
  tests/test_gramstack_sdk_local.py tests/test_m1.py tests/test_m3.py
```

Result: **31 passed, 1 skipped**. Three connector integration tests cover the
portal handoff, consent gate, exact fields, one-attempt retrieval and uncertain
delivery. The skip is an optional live Bhashini test. The connector and its tests
remain local to that checkout; these commands document that experiment rather
than claim an upstream integration already exists.

The current registry profile is `shg.membership`. The playground's other Excel,
SQL and Google Sheets resources need registered adapters and explicit resource
profiles before GRAMStack can mediate them. No such migration is claimed here.
