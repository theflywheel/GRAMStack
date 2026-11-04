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
  const sessions = new Map();
  const capture = (async () => {
    while (!stopped) {
      const start = Date.now();
      const session = active;
      if (session) {
        const { data } = await session.send("Page.captureScreenshot", {
          format: "jpeg",
          quality: 86,
          captureBeyondViewport: false,
        });
        if (active === session && !stopped) {
          await new Promise((resolve, reject) => {
            encoder.stdin.write(Buffer.from(data, "base64"), (error) =>
              error ? reject(error) : resolve(),
            );
          });
        }
      }
      await delay(Math.max(0, 100 - (Date.now() - start)));
    }
  })();
  capture.catch(() => {});
  encoder.stdin.on("error", () => {});
  return {
    async show(page) {
      if (!sessions.has(page)) {
        sessions.set(page, await page.context().newCDPSession(page));
      }
      active = sessions.get(page);
    },
    pause() {
      active = undefined;
    },
    async stop() {
      stopped = true;
      try {
        await capture;
      } finally {
        encoder.stdin.end();
      }
      const [code] = await exited;
      if (code !== 0) throw new Error("Demo video encoding failed");
    },
  };
}
