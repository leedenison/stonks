import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { refusal } from "./refusal";

describe("refusal", () => {
  it("gives the service's reason for a failed precondition", () => {
    const err = new ConnectError(
      "no such integration",
      Code.FailedPrecondition,
    );
    expect(refusal(err, "fallback")).toBe("no such integration");
  });

  it("gives the fallback for any other failure", () => {
    expect(
      refusal(new ConnectError("down", Code.Unavailable), "fallback"),
    ).toBe("fallback");
    expect(refusal(null, "fallback")).toBe("fallback");
  });
});
