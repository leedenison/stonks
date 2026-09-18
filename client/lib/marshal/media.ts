// mediaType reduces a media type as a browser reports it on a File to the
// bare lower-case type, so "Text/CSV; charset=utf-8" compares as "text/csv".
export function mediaType(type: string): string {
  return type.split(";", 1)[0].trim().toLowerCase();
}
