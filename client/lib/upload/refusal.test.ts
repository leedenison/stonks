import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { refusal } from "./refusal";

describe("refusal", () => {
  it("quotes the service's reason for refusing the statement", () => {
    expect(
      refusal(
        new ConnectError(
          "order_from must precede order_before",
          Code.InvalidArgument,
        ),
      ),
    ).toBe("Statement rejected: order_from must precede order_before");
  });

  it("offers a retry for any other failure", () => {
    expect(refusal(new ConnectError("down", Code.Unavailable))).toBe(
      "The upload failed. Try again.",
    );
    expect(refusal(new Error("boom"))).toBe("The upload failed. Try again.");
  });
});
