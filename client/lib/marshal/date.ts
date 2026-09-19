// Dates cross the wire as ISO 8601 strings with no time. Nothing here builds a
// Date from an export's local time, so no zone conversion moves a date. The
// one Date read from the clock is read in the browser's zone, so today is the
// user's calendar date.

const MONTHS: Record<string, string> = {
  Jan: "01",
  Feb: "02",
  Mar: "03",
  Apr: "04",
  May: "05",
  Jun: "06",
  Jul: "07",
  Aug: "08",
  Sep: "09",
  Oct: "10",
  Nov: "11",
  Dec: "12",
};

export function iso(year: string, month: string, day: string): string {
  return `${year}-${month.padStart(2, "0")}-${day.padStart(2, "0")}`;
}

// monthNumber maps a three letter English month name to its two digit number;
// undefined for anything else.
export function monthNumber(name: string): string | undefined {
  return MONTHS[name];
}

// today is the calendar date of now in the browser's zone.
export function today(now = new Date()): string {
  return iso(
    String(now.getFullYear()),
    String(now.getMonth() + 1),
    String(now.getDate()),
  );
}

export function nextDay(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d + 1)).toISOString().slice(0, 10);
}

export function prevDay(date: string): string {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d - 1)).toISOString().slice(0, 10);
}

export function maxDate(a: string, b: string): string {
  return a > b ? a : b;
}

export function minDate(a: string, b: string): string {
  return a < b ? a : b;
}
