// enumLabel renders v as lower case words, for example "failed permanent"
// for FAILED_PERMANENT.
export function enumLabel(e: Record<number, string>, v: number): string {
  return (e[v] ?? "").toLowerCase().replaceAll("_", " ");
}

// enumValues lists the values of e that a filter offers. It omits
// UNSPECIFIED.
export function enumValues(e: Record<number, string>): number[] {
  return Object.keys(e)
    .map(Number)
    .filter((v) => !Number.isNaN(v) && v !== 0);
}

// enumParam names v in an address as its lower case name, for example
// "statement" for RunKind.STATEMENT.
export function enumParam(e: Record<number, string>, v: number): string {
  return (e[v] ?? "").toLowerCase();
}

// fromParam reads a name enumParam wrote. It returns undefined for an unknown
// name and for UNSPECIFIED.
export function fromParam(
  e: Record<string, string | number>,
  s: string,
): number | undefined {
  const v = e[s.toUpperCase()];
  return typeof v === "number" && v !== 0 ? v : undefined;
}
