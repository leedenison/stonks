import { Code, ConnectError } from "@connectrpc/connect";

// refusal is the text to show for a failed call. It carries the service's
// reason when the service refused the call, and fallback otherwise.
export function refusal(error: Error | null, fallback: string): string {
  if (error instanceof ConnectError && error.code === Code.FailedPrecondition) {
    return error.rawMessage;
  }
  return fallback;
}
