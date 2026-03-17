import { ENRICH_INTERVAL_MS, SOUNDCHARTS_CREDENTIALS } from "./config.ts";
import { runEnrichCycle } from "./enricher.ts";

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

if (import.meta.main) {
  console.log(
    `worker-enrich started. Interval: ${ENRICH_INTERVAL_MS / 1000}s, ` +
      `Soundcharts credential sets: ${SOUNDCHARTS_CREDENTIALS.length}`,
  );

  const ac = new AbortController();
  const { signal } = ac;

  const onSignal = () => {
    console.log("worker-enrich: shutdown signal received");
    ac.abort();
    if (Deno.build.os === "windows") {
      Deno.removeSignalListener("SIGINT", onSignal);
      try {
        Deno.removeSignalListener("SIGBREAK", onSignal);
      } catch {
        // ignore
      }
    } else {
      Deno.removeSignalListener("SIGINT", onSignal);
      Deno.removeSignalListener("SIGTERM", onSignal);
    }
  };

  if (Deno.build.os === "windows") {
    Deno.addSignalListener("SIGINT", onSignal);
    try {
      Deno.addSignalListener("SIGBREAK", onSignal);
    } catch {
      // ignore
    }
  } else {
    Deno.addSignalListener("SIGINT", onSignal);
    Deno.addSignalListener("SIGTERM", onSignal);
  }

  // Run immediately on startup, then on a fixed interval until signaled.
  (async () => {
    while (!signal.aborted) {
      try {
        await runEnrichCycle();
      } catch (err) {
        console.error("Enrich cycle failed:", err);
      }

      if (signal.aborted) break;
      await sleep(ENRICH_INTERVAL_MS, signal);
    }

    console.log("worker-enrich: exiting cleanly");
  })();
}
