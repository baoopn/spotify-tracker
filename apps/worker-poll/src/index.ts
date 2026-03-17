import { runPoller } from "./poller.ts";
import { info, error } from "@shared/logger.ts";

if (import.meta.main) {
  // lightweight health check mode for container healthprobes
  if (Deno.args.includes("--health")) {
    console.log("ok");
    Deno.exit(0);
  }
  const ac = new AbortController();
  const { signal } = ac;

  const onSignal = () => {
    info("worker-poll: shutdown signal received");
    ac.abort();
    if (Deno.build.os === "windows") {
      Deno.removeSignalListener("SIGINT", onSignal);
      // SIGBREAK is supported on Windows and mirrors ctrl-break
      try {
        Deno.removeSignalListener("SIGBREAK", onSignal);
      } catch {
        // ignore if not available
      }
    } else {
      Deno.removeSignalListener("SIGINT", onSignal);
      Deno.removeSignalListener("SIGTERM", onSignal);
    }
  };

  if (Deno.build.os === "windows") {
    Deno.addSignalListener("SIGINT", onSignal);
    // SIGBREAK is ctrl-break on Windows
    try {
      Deno.addSignalListener("SIGBREAK", onSignal);
    } catch {
      // ignore if not available
    }
  } else {
    Deno.addSignalListener("SIGINT", onSignal);
    Deno.addSignalListener("SIGTERM", onSignal);
  }

  try {
    await runPoller(signal);
  } catch (err) {
    error("Unhandled error in runPoller:", err);
    throw err;
  }
}
