"""Create a private configuration for one fresh, local eSignet project."""
import argparse
import os
from pathlib import Path
import secrets
import socket
from contextlib import ExitStack


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="new private state directory")
    args = parser.parse_args()
    args.directory.mkdir(mode=0o700)
    values = {"COMPOSE_PROJECT_NAME": "gramstack-esignet-" + secrets.token_hex(6)}
    with ExitStack() as sockets:
        for kind in ("API", "MOCK", "UI", "DB"):
            listener = sockets.enter_context(socket.socket())
            listener.bind(("127.0.0.1", 0))
            values[f"ESIGNET_DEMO_{kind}_PORT"] = str(listener.getsockname()[1])
        for key in ("DB_PASSWORD", "KEY_PASSWORD", "MOCK_KEY_PASSWORD"):
            values[f"ESIGNET_DEMO_{key}"] = secrets.token_hex(24)
        values["ESIGNET_DEMO_REDIS_PASSWORD"] = secrets.token_hex(32)
        fd = os.open(args.directory / ".env", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="ascii") as output:
            for key, value in values.items():
                output.write(f"{key}={value}\n")


if __name__ == "__main__":
    main()
