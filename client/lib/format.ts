import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampDate } from "@bufbuild/protobuf/wkt";

// formatInstant renders a timestamp to the minute in UTC, the zone every
// time in the API is stated in.
export function formatInstant(ts: Timestamp): string {
  return `${timestampDate(ts).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

// formatElapsed renders a duration in milliseconds as the two largest units
// that are not zero, so a running run reads "1m 05s" and a long one "2h 10m".
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) {
    return `${h}h ${String(m).padStart(2, "0")}m`;
  }
  if (m > 0) {
    return `${m}m ${String(s).padStart(2, "0")}s`;
  }
  return `${s}s`;
}
