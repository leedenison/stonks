import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampDate } from "@bufbuild/protobuf/wkt";

// formatInstant renders a timestamp to the minute in UTC, the zone every
// time in the API is stated in.
export function formatInstant(ts: Timestamp): string {
  return `${timestampDate(ts).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}
