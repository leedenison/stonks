// OCC option symbols: a root padded to six characters, the expiry as YYMMDD,
// C or P, and the strike in thousandths as eight digits, 21 characters in all.

const OCC = /^(.{6})(\d{6})([CP])(\d{8})$/;

export function isOcc(symbol: string): boolean {
  return OCC.test(symbol);
}

// buildOcc names the contract the terms describe under root, or undefined
// when the strike has more than three decimals or exceeds eight digits. The
// expiry is YYYYMMDD and the type is CALL or PUT.
export function buildOcc(
  root: string,
  expiry: string,
  type: string,
  strike: string,
): string | undefined {
  const cp = { CALL: "C", PUT: "P" }[type];
  const [whole, frac = ""] = strike.split(".");
  if (
    !cp ||
    !/^\d{8}$/.test(expiry) ||
    frac.length > 3 ||
    !/^\d*$/.test(whole + frac)
  ) {
    return undefined;
  }
  const thousandths = (whole + frac.padEnd(3, "0")).replace(/^0+(?=\d)/, "");
  if (thousandths.length > 8 || root.length > 6) return undefined;
  return `${root.padEnd(6)}${expiry.slice(2)}${cp}${thousandths.padStart(8, "0")}`;
}
