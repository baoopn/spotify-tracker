import { POLL_INTERVAL_MS } from "./config.ts";
import { syncCurrentlyPlaying } from "./sync.ts";

function sleep(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve) => {
    if (signal?.aborted) return resolve();
    const id = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    function onAbort() {
      clearTimeout(id);
      resolve();
    }
    signal?.addEventListener("abort", onAbort);
  });
}

export async function runPoller(signal?: AbortSignal): Promise<void> {
  console.log(`worker-poll started. Poll interval: ${POLL_INTERVAL_MS}ms`);

  while (!signal?.aborted) {
    try {
      await syncCurrentlyPlaying();
    } catch (error) {
      console.error("Poll cycle failed:", error);
    }

    if (signal?.aborted) break;
    await sleep(POLL_INTERVAL_MS, signal);
  }

  console.log("worker-poll: exiting cleanly");
}
