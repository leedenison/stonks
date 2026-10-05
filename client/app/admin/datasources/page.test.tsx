import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AdminService,
  DatasourceSchema,
  ListDatasourcesResponseSchema,
  UpdateDatasourceResponseSchema,
} from "@/gen/admin/v1/admin_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import DatasourcesPage from "./page";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

const openfigi = create(DatasourceSchema, {
  name: "openfigi",
  enabled: true,
  precedence: 1,
  endpoint: "http://stub",
  hasCredential: true,
});
const other = create(DatasourceSchema, { name: "other", precedence: 2 });

function serving(updateDatasource = vi.fn()) {
  updateDatasource.mockReturnValue(
    create(UpdateDatasourceResponseSchema, { datasource: openfigi }),
  );
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(AdminService, {
      listDatasources: () =>
        create(ListDatasourcesResponseSchema, {
          datasources: [openfigi, other],
        }),
      updateDatasource,
    });
  });
}

describe("DatasourcesPage", () => {
  it("lists the datasources in precedence order with their state", async () => {
    renderWithAuth(<DatasourcesPage />, serving());
    await waitFor(() =>
      expect(screen.getByTestId("datasource-row-openfigi")).toBeTruthy(),
    );
    const rows = screen
      .getByTestId("admin-datasources-table")
      .querySelectorAll("tbody tr");
    expect(rows[0]?.getAttribute("data-testid")).toBe(
      "datasource-row-openfigi",
    );
    expect(rows[1]?.getAttribute("data-testid")).toBe("datasource-row-other");
    const row = screen.getByTestId("datasource-row-openfigi");
    expect(row.textContent).toContain("enabled");
    expect(row.textContent).toContain("http://stub");
    expect(row.textContent).toContain("held");
    expect(screen.getByTestId("datasource-grip-openfigi")).toBeTruthy();
    expect(screen.getByTestId("datasource-row-other").textContent).toContain(
      "none",
    );
  });

  it("toggles a datasource's state, keeping its endpoint", async () => {
    const updateDatasource = vi.fn();
    renderWithAuth(<DatasourcesPage />, serving(updateDatasource));
    await waitFor(() =>
      expect(screen.getByTestId("datasource-toggle-openfigi")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-toggle-openfigi"));
    await waitFor(() =>
      expect(updateDatasource).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "openfigi",
          enabled: false,
          endpoint: "http://stub",
        }),
        expect.anything(),
      ),
    );
  });

  it("edits the endpoint and replaces the credential", async () => {
    const updateDatasource = vi.fn();
    renderWithAuth(<DatasourcesPage />, serving(updateDatasource));
    await waitFor(() =>
      expect(screen.getByTestId("datasource-edit-openfigi")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-edit-openfigi"));
    fireEvent.change(screen.getByTestId("datasource-endpoint"), {
      target: { value: "http://elsewhere" },
    });
    fireEvent.change(screen.getByTestId("datasource-credential"), {
      target: { value: "key" },
    });
    fireEvent.click(screen.getByTestId("datasource-save"));
    await waitFor(() =>
      expect(updateDatasource).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "openfigi",
          enabled: true,
          endpoint: "http://elsewhere",
          credential: "key",
        }),
        expect.anything(),
      ),
    );
  });

  it("clears the credential held", async () => {
    const updateDatasource = vi.fn();
    renderWithAuth(<DatasourcesPage />, serving(updateDatasource));
    await waitFor(() =>
      expect(screen.getByTestId("datasource-edit-openfigi")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-edit-openfigi"));
    fireEvent.click(screen.getByTestId("datasource-clear-credential"));
    fireEvent.click(screen.getByTestId("datasource-save"));
    await waitFor(() =>
      expect(updateDatasource).toHaveBeenCalledWith(
        expect.objectContaining({ name: "openfigi", credential: "" }),
        expect.anything(),
      ),
    );
  });

  it("shows a refusal to enable a datasource", async () => {
    const updateDatasource = vi.fn();
    const transport = serving(updateDatasource);
    updateDatasource.mockImplementation(() => {
      throw new ConnectError("no such integration", Code.FailedPrecondition);
    });
    renderWithAuth(<DatasourcesPage />, transport);
    await waitFor(() =>
      expect(screen.getByTestId("datasource-toggle-other")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-toggle-other"));
    await waitFor(() =>
      expect(screen.getByTestId("datasource-update-error")).toBeTruthy(),
    );
    expect(updateDatasource).toHaveBeenCalledWith(
      expect.objectContaining({ name: "other", enabled: true }),
      expect.anything(),
    );
  });
  it("shows a refused save in the dialog, which stays open", async () => {
    const updateDatasource = vi.fn();
    const transport = serving(updateDatasource);
    updateDatasource.mockImplementation(() => {
      throw new ConnectError(
        "the integration refuses",
        Code.FailedPrecondition,
      );
    });
    renderWithAuth(<DatasourcesPage />, transport);
    await waitFor(() =>
      expect(screen.getByTestId("datasource-edit-openfigi")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-edit-openfigi"));
    fireEvent.click(screen.getByTestId("datasource-save"));
    const notice = await screen.findByTestId("datasource-dialog-error");
    expect(notice.textContent).toContain("the integration refuses");
    expect(
      screen.getByTestId("datasource-dialog").contains(notice),
    ).toBeTruthy();
    expect(screen.queryByTestId("datasource-update-error")).toBeNull();
  });
});
