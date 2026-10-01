import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  DatasourceSchema,
  ListDatasourcesResponseSchema,
} from "@/gen/admin/v1/admin_pb";
import { reordered } from "./datasources";

describe("reordered", () => {
  it("orders the listing by the names and numbers the precedence", () => {
    const list = create(ListDatasourcesResponseSchema, {
      datasources: [
        create(DatasourceSchema, { name: "alpha", precedence: 1 }),
        create(DatasourceSchema, { name: "beta", precedence: 2 }),
      ],
    });
    const got = reordered(list, ["beta", "alpha", "gamma"]);
    expect(got.datasources.map((d) => [d.name, d.precedence])).toEqual([
      ["beta", 1],
      ["alpha", 2],
    ]);
  });
});
