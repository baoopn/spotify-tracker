import { runPoller } from "./poller.ts";

if (import.meta.main) {
  const ac = new AbortController();
  const { signal } = ac;

  const onSignal = () => {
    console.log("worker-poll: shutdown signal received");
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

  await runPoller(signal);
}
