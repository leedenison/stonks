import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  RowSchema,
  StatementItemSchema,
} from "@/gen/statement/v1/statement_pb";
import { AssetClass, StatedKeySchema } from "@/gen/type/v1/type_pb";
import { RejectionGroups } from "./rejection-groups";

const gbp = create(StatedKeySchema, {
  assetClass: AssetClass.CASH,
  currency: "GBP",
});

const items = [
  create(StatementItemSchema, {
    ordinal: 2,
    reason: "order date outside the claimed period",
    row: create(RowSchema, {
      key: gbp,
      orderDate: "2025-01-15",
      quantity: "-5.40",
    }),
  }),
  create(StatementItemSchema, {
    ordinal: 5,
    reason: "no key",
    row: create(RowSchema, { orderDate: "2025-02-10", quantity: "1" }),
  }),
  create(StatementItemSchema, {
    ordinal: 3,
    reason: "order date outside the claimed period",
    row: create(RowSchema, {
      key: gbp,
      orderDate: "2025-01-22",
      quantity: "100",
    }),
  }),
];

describe("RejectionGroups", () => {
  it("lists each reason with its count, closed, and opens to its rows", () => {
    render(<RejectionGroups items={items} />);
    expect(screen.getAllByTestId(/^rejection-group-/).length).toBe(2);
    expect(screen.getByTestId("rejection-count-0").textContent).toBe("2");
    expect(screen.getByTestId("rejection-reason-1").textContent).toBe("no key");
    expect(screen.getByTestId("rejection-count-1").textContent).toBe("1");
    const first = screen.getByTestId("rejection-group-0") as HTMLDetailsElement;
    expect(first.open).toBe(false);
    fireEvent.click(first.querySelector("summary") as HTMLElement);
    expect(first.open).toBe(true);
    const row = screen.getByTestId("rejection-row-2");
    expect(row.textContent).toContain("2025-01-15");
    expect(row.textContent).toContain("GBP");
    expect(row.textContent).toContain("-5.40");
  });

  it("starts a lone reason open", () => {
    render(<RejectionGroups items={[items[1]]} />);
    const only = screen.getByTestId("rejection-group-0") as HTMLDetailsElement;
    expect(only.open).toBe(true);
    expect(screen.getByTestId("rejection-row-5")).toBeTruthy();
  });
});
