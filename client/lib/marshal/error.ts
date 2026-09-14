// MarshalError names the line of the export at fault where the format has
// lines, and the kind of line where the kind is what is unknown.
export class MarshalError extends Error {
  constructor(
    message: string,
    readonly line?: number,
    readonly kind?: string,
  ) {
    super(line === undefined ? message : `line ${line}: ${message}`);
    this.name = "MarshalError";
  }
}
