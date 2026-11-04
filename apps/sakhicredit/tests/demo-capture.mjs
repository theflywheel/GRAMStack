import { spawn } from "node:child_process";
import { once } from "node:events";
import { setTimeout as delay } from "node:timers/promises";

// Capture the active demo tab directly from Chromium. The driver switches
// tabs explicitly and pauses before entering the fictional login values.
export function captureDemo(output) {
  const encoder = spawn(
    process.env.FFMPEG_BIN || "ffmpeg",
    [
      "-hide_banner",
      "-loglevel",
      "error",
      "-n",
      "-f",
      "image2pipe",
      "-framerate",
      "10",
      "-vcodec",
      "mjpeg",
      "-i",
      "pipe:0",
      "-c:v",
      "libx264",
      "-pix_fmt",
      "yuv420p",
      "-movflags",
      "+faststart",
      output,
    ],
    { stdio: ["pipe", "ignore", "inherit"] },
  );
  const exited = once(encoder, "exit");
  exited.catch(() => {});
  let active;
  let stopped = false;
  let captureError;
  const capture = (async () => {
    while (!stopped) {
      const start = Date.now();
      const page = active;
      if (page) {
        let frame;
        try {
          frame = await page.screenshot({
            type: "jpeg",
            quality: 86,
            style:
              ".demo-info p:first-of-type { visibility: hidden !important; }",
            timeout: 1000,
          });
        } catch (error) {
          if (error.name !== "TimeoutError" && active === page && !stopped)
            throw error;
        }
        if (frame && active === page && !stopped) {
          await new Promise((resolve, reject) => {
            encoder.stdin.write(frame, (error) =>
              error ? reject(error) : resolve(),
            );
          });
        }
      }
      await delay(Math.max(0, 100 - (Date.now() - start)));
    }
  })();
  capture.catch((error) => {
    if (!stopped) captureError = error;
  });
  encoder.stdin.on("error", () => {});
  return {
    show(page) {
      active = page;
    },
    pause() {
      active = undefined;
    },
    async stop() {
      stopped = true;
      // The stop flag prevents an in-flight screenshot from writing after
      // stdin is closed.
      encoder.stdin.end();
      const [code] = await exited;
      if (captureError) throw captureError;
      if (code !== 0) throw new Error("Demo video encoding failed");
    },
  };
}
