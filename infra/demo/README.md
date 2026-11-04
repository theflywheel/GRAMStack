# Standalone SakhiCredit demo

Run this from the repository root with Python 3.10+, Go 1.26.8, Docker Compose,
a TLS 1.3 capable curl, PostgreSQL 15 binaries and an official checksum-verified Hydra v26.2.0
executable. Check out DeDi-node at
`dbdb9f6ce8f69b8253f405f9c14ea345cd9e6d1f` with no tracked modifications.

```sh
state=$(mktemp -d /tmp/gramstack-demo.XXXXXX)
python3 infra/demo/run.py "$state/demo" \
  --pg-bin /path/to/postgresql/bin \
  --hydra /path/to/hydra \
  --dedi-source /path/to/DeDi-node
```

The launcher starts official eSignet with its official mock identity backend
in a separate Compose project. It builds the Go services and pinned DeDi
node, creates private SQL databases, runs explicit migrations and registers
the demo clients. It then starts the fictional LokOS source, SDK adapter,
gateway and SakhiCredit product. Fresh ports and credentials are generated for
each run. All listeners use loopback or a private Unix socket. Adapter, source
and data transport use the generated mTLS identities.

Open the printed SakhiCredit URL. Start an application, review the sharing
request and sign in via UIN/VID with fictional ID `9000000001` and OTP
`111111`. Verify registry membership, confirm the notice and approve. Return
to SakhiCredit and select **Get approved details**. With both fields selected,
the fictional source returns active membership and ₹12,500.75. The signed
disclosure evidence is retained before the approved result is released.
The gateway worker publishes its commitment to DeDi asynchronously.

For a headless check of the running stack, install the pinned development
driver and run the browser smoke script in another terminal:

```sh
npm ci --ignore-scripts --prefix apps/sakhicredit
CHROME_BIN=/path/to/chrome node apps/sakhicredit/tests/demo-smoke.mjs "$state/demo"
```

The script exercises the actual product, identity UI and consent flow and
saves the approved result screenshot in the private state directory.
Use the existing integration runners for the full denial and outage suite.

To rehearse recording the same browser journey, set
`GRAMSTACK_DEMO_RECORDING="$state/demo/rehearsal.mp4"` and rerun the smoke
command. Use a working FFmpeg executable with the H.264 encoder available.
`FFMPEG_BIN` can point to that executable. The capture follows the active
browser tab, pauses during fixture credential entry, and creates an MP4
without overwriting existing footage. Use `ffprobe` to check the video stream
and play the file in a browser. This rehearsal covers the approved journey.
The final demo also needs a denial or replay case and the capability matrix.

Ctrl-C stops the processes and the launcher's Compose project. The private
state directory and eSignet volumes remain available for inspection. Use a
new directory for each launch. The launcher is for fresh local demos.
Railway uses separate deployments and reviewed public/private routes.
