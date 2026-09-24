import { Code, ConnectError } from "@connectrpc/connect";

// refusal is what the user reads when creating a statement fails: the
// service's reason where it refused the statement, and otherwise a retry.
export function refusal(err: Error): string {
  if (err instanceof ConnectError && err.code === Code.InvalidArgument) {
    return `Statement rejected: ${err.rawMessage}`;
  }
  return "The upload failed. Try again.";
}
