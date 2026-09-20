import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { RowSchema, StatementSchema } from "@/gen/statement/v1/statement_pb";
import { claim, period } from "./period";

const statement = create(StatementSchema, {
  orderFrom: "2025-01-01",
  orderBefore: "2025-03-17",
  rows: ["2025-01-02", "2025-02-01", "2025-03-16"].map((orderDate) =>
    create(RowSchema, { orderDate, quantity: "1" }),
  ),
});

describe("period", () => {
  it("defaults to the stated period with an inclusive end", () => {
    expect(period(statement)).toEqual({
      from: "2025-01-01",
      to: "2025-03-16",
      outside: 0,
      valid: true,
    });
  });

  it("counts the rows a narrowed period leaves out on either side", () => {
    expect(
      period(statement, { from: "2025-02-01", to: "2025-03-16" }),
    ).toMatchObject({
      outside: 1,
      valid: true,
    });
    expect(
      period(statement, { from: "2025-01-03", to: "2025-03-15" }),
    ).toMatchObject({
      outside: 2,
      valid: true,
    });
  });

  it("is invalid while an end is blank or the period is reversed", () => {
    expect(period(statement, { from: "", to: "2025-03-16" }).valid).toBe(false);
    expect(period(statement, { from: "2025-01-01", to: "" }).valid).toBe(false);
    expect(
      period(statement, { from: "2025-03-16", to: "2025-01-01" }).valid,
    ).toBe(false);
    expect(
      period(statement, { from: "2025-02-01", to: "2025-02-01" }).valid,
    ).toBe(true);
  });
});

describe("claim", () => {
  it("restates the statement with an exclusive end", () => {
    const claimed = claim(statement, { from: "2025-02-01", to: "2025-03-16" });
    expect(claimed.orderFrom).toBe("2025-02-01");
    expect(claimed.orderBefore).toBe("2025-03-17");
    expect(claimed.rows).toEqual(statement.rows);
  });
});
