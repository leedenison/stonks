import { create } from "@bufbuild/protobuf";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  IdentifierSchema,
  IdentifierType,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import {
  IdentifierChip,
  StatedKeyChips,
  identifierLabel,
} from "./identifier-chip";

const isin = create(IdentifierSchema, {
  type: IdentifierType.ISIN,
  value: "GB00B03MLX29",
});
const ticker = create(IdentifierSchema, {
  type: IdentifierType.MIC_TICKER,
  domain: "XLON",
  value: "SHEL",
});

describe("IdentifierChip", () => {
  it("shows the type, the value and the domain", () => {
    const { container } = render(<IdentifierChip id={ticker} />);
    const chip = container.firstElementChild;
    expect(chip?.getAttribute("data-identifier-type")).toBe("MIC_TICKER");
    expect(chip?.textContent).toBe("TickerSHEL(XLON)");
  });

  it("leaves out an absent domain", () => {
    const { container } = render(<IdentifierChip id={isin} />);
    expect(container.textContent).toBe("ISINGB00B03MLX29");
  });

  it("labels each type, and an unknown type as nothing", () => {
    expect(identifierLabel(99 as IdentifierType)).toBe("");
    expect(identifierLabel(IdentifierType.OPENFIGI_SHARE_CLASS)).toBe(
      "FIGI share class",
    );
  });
});

describe("StatedKeyChips", () => {
  it("renders a chip per identifier", () => {
    const description = create(IdentifierSchema, {
      type: IdentifierType.BROKER_DESCRIPTION,
      value: "SHELL PLC",
      domain: "ibkr",
    });
    const { container } = render(
      <StatedKeyChips
        statedKey={create(StatedKeySchema, {
          identifiers: [isin, ticker, description],
        })}
      />,
    );
    expect(container.querySelectorAll("[data-identifier-type]")).toHaveLength(
      3,
    );
    expect(container.textContent).toContain("Description");
    expect(container.textContent).toContain("SHELL PLC");
  });

  it("renders nothing without a key", () => {
    const { container } = render(<StatedKeyChips />);
    expect(container.textContent).toBe("");
  });
});
