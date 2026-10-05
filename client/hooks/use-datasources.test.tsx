import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  AdminService,
  DatasourceSchema,
  type ListDatasourcesResponse,
  ListDatasourcesResponseSchema,
  ReorderDatasourcesResponseSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useDatasources, useReorderDatasources } from "./use-datasources";

const names = (d: ListDatasourcesResponse | undefined) =>
  d?.datasources.map((s) => s.name);

// serving lists alpha then beta once and never answers a later listing.
// After a reorder, the listing therefore shows only what the hook wrote.
// asked reports whether the service has the reorder, and finish settles it.
function serving() {
  let finish: ((err?: Error) => void) | undefined;
  let listed = false;
  const transport = transportWith(
    liveSession({ role: Role.ADMIN }),
    ({ service }) => {
      service(AdminService, {
        listDatasources: () => {
          if (listed) return new Promise(() => {});
          listed = true;
          return create(ListDatasourcesResponseSchema, {
            datasources: [
              create(DatasourceSchema, { name: "alpha", precedence: 1 }),
              create(DatasourceSchema, { name: "beta", precedence: 2 }),
            ],
          });
        },
        reorderDatasources: () =>
          new Promise((resolve, reject) => {
            finish = (err) =>
              err
                ? reject(err)
                : resolve(create(ReorderDatasourcesResponseSchema, {}));
          }),
      });
    },
  );
  return {
    transport,
    asked: () => finish !== undefined,
    finish: (err?: Error) => finish?.(err),
  };
}

describe("useReorderDatasources", () => {
  it("shows the new order at once and puts the old one back when refused", async () => {
    const { transport, asked, finish } = serving();
    const { result } = renderHook(
      () => ({ list: useDatasources(), reorder: useReorderDatasources() }),
      { wrapper: authWrapper(transport) },
    );
    await waitFor(() =>
      expect(names(result.current.list.data)).toEqual(["alpha", "beta"]),
    );

    act(() => result.current.reorder.mutate(["beta", "alpha"]));
    await waitFor(() =>
      expect(names(result.current.list.data)).toEqual(["beta", "alpha"]),
    );

    await waitFor(() => expect(asked()).toBe(true));
    act(() => finish(new ConnectError("refused", Code.InvalidArgument)));
    await waitFor(() =>
      expect(names(result.current.list.data)).toEqual(["alpha", "beta"]),
    );
  });
});
