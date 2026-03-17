export type LogArg = unknown;

function ts(): string {
  return new Date().toISOString();
}

function format(prefix: string, args: LogArg[]) {
  return [`[${ts()}] ${prefix}`, ...args];
}

export function info(...args: LogArg[]) {
  console.log(...format("INFO", args));
}

export function warn(...args: LogArg[]) {
  console.warn(...format("WARN", args));
}

export function error(...args: LogArg[]) {
  console.error(...format("ERROR", args));
}

export function debug(...args: LogArg[]) {
  // keep debug as info-level output; consumers can change this later
  console.log(...format("DEBUG", args));
}

export default { info, warn, error, debug };
