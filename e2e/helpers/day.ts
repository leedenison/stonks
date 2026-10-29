import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";

// utcDay is the UTC day of a server timestamp, as YYYY-MM-DD. The server
// takes today from its own clock, so a spec reads the day from a timestamp
// the server wrote.
export function utcDay(t: Timestamp): string {
  return timestampDate(t).toISOString().slice(0, 10);
}

// nextDay is the day after day, as YYYY-MM-DD.
export function nextDay(day: string): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + 1);
  return d.toISOString().slice(0, 10);
}
