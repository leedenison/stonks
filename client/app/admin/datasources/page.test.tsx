import { create } from "@bufbuild/protobuf";
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AdminService,
  DatasourceSchema,
  ListDatasourcesResponseSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import DatasourcesPage from "./page";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

describe("DatasourcesPage", () => {
  it("lists the datasources with their state", async () => {
    renderWithAuth(
      <DatasourcesPage />,
      transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
        service(AdminService, {
          listDatasources: () =>
            create(ListDatasourcesResponseSchema, {
              datasources: [
                create(DatasourceSchema, {
                  name: "openfigi",
                  enabled: true,
                  precedence: 10,
                  endpoint: "http://stub",
                }),
              ],
            }),
        });
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("datasource-row-openfigi")).toBeTruthy(),
    );
    const row = screen.getByTestId("datasource-row-openfigi");
    expect(row.textContent).toContain("enabled");
    expect(row.textContent).toContain("http://stub");
  });
});
