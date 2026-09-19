// Exact arithmetic on decimal strings, so no quantity or amount passes
// through a float between the export and the wire.

interface Scaled {
  units: bigint;
  scale: number;
}

const DECIMAL = /^([+-]?)(\d*)(?:\.(\d*))?$/;

function parse(s: string): Scaled {
  const m = DECIMAL.exec(s.replace(/[$,\s]/g, ""));
  if (!m || (m[2] === "" && !m[3])) throw new Error(`not a decimal: ${s}`);
  const frac = m[3] ?? "";
  const units = BigInt(m[2] + frac);
  return { units: m[1] === "-" ? -units : units, scale: frac.length };
}

function render({ units, scale }: Scaled): string {
  const digits = (units < 0n ? -units : units)
    .toString()
    .padStart(scale + 1, "0");
  const whole = digits.slice(0, digits.length - scale);
  const frac = digits.slice(digits.length - scale).replace(/0+$/, "");
  return `${units < 0n ? "-" : ""}${whole}${frac ? `.${frac}` : ""}`;
}

function rescale(a: Scaled, scale: number): bigint {
  return a.units * 10n ** BigInt(scale - a.scale);
}

// normalise parses an amount as an export states it, allowing a currency
// sign, thousands separators and a leading dot, and returns it in canonical
// form: no sign on zero, no leading zeros, no trailing fraction zeros.
export function normalise(s: string): string {
  return render(parse(s));
}

export function add(a: string, b: string): string {
  const x = parse(a);
  const y = parse(b);
  const scale = Math.max(x.scale, y.scale);
  return render({ units: rescale(x, scale) + rescale(y, scale), scale });
}

export function negate(a: string): string {
  const x = parse(a);
  return render({ units: -x.units, scale: x.scale });
}

export function isZero(a: string): boolean {
  return parse(a).units === 0n;
}

// toFixed rounds to places decimal places, half away from zero, and keeps
// the trailing zeros, so a column of quantities aligns.
export function toFixed(a: string, places: number): string {
  const x = parse(a);
  let units: bigint;
  if (x.scale > places) {
    const div = 10n ** BigInt(x.scale - places);
    const q = x.units / div;
    const r = x.units % div;
    const half = (r < 0n ? -r : r) * 2n >= div;
    units = half ? q + (x.units < 0n ? -1n : 1n) : q;
  } else {
    units = rescale(x, places);
  }
  const digits = (units < 0n ? -units : units)
    .toString()
    .padStart(places + 1, "0");
  const whole = digits.slice(0, digits.length - places);
  const frac = digits.slice(digits.length - places);
  return `${units < 0n ? "-" : ""}${whole}${places ? `.${frac}` : ""}`;
}
