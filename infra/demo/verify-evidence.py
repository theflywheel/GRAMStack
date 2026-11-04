"""Verify retained commitments from a running standalone demo."""
import argparse
import json
import os
from pathlib import Path
import subprocess
from urllib.parse import urlencode, urlsplit

ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("state", type=Path)
    state = parser.parse_args().state.resolve()
    tmp = Path("/tmp").resolve()
    if state == tmp or not state.is_relative_to(tmp) or not state.relative_to(tmp).parts[0].startswith("gramstack-demo"):
        parser.error("Expected an isolated GRAMStack demo state directory")
    endpoints = json.loads((state / "endpoints.json").read_text())
    pg = urlsplit(endpoints["pg"])
    if pg.hostname != "127.0.0.1" or not pg.port or not (state / f".s.PGSQL.{pg.port}").is_socket():
        parser.error("Expected the running demo's private PostgreSQL socket")
    env = os.environ.copy()
    for key in ("DEDI_ADMIN_PASSWORD", "DEDI_PUBLISHER_KEY_FILE", "DEDI_NODE_URL"):
        env.pop(key, None)
    env.update(
        DATABASE_URL="postgres://gramstack@localhost/core?" + urlencode(
            dict(host=str(state), port=pg.port, sslmode="disable")
        ),
        DEDI_NAMESPACE="gramstack-demo",
        DEDI_REGISTRY="commitments",
        DEDI_PUBLISHER_KEY_ID="gramstack-demo",
        DEDI_CHECKPOINT_ORIGIN="gramstack-demo/log",
        DEDI_CHECKPOINT_VERIFIER=(state / "checkpoint.txt").read_text().strip(),
    )
    result = subprocess.run(
        ["go", "run", "./services/gateway/cmd/gramstack", "verify-anchors"],
        cwd=ROOT, env=env, timeout=45,
    )
    raise SystemExit(result.returncode)


if __name__ == "__main__":
    main()
