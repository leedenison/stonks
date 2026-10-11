import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  DatasourceSchema,
  DatasourceService,
  ListDatasourcesResponseSchema,
  UpdateDatasourceResponseSchema,
} from "@/gen/admin/datasource/v1/datasource_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import DatasourcesPage from "./page";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

const openfigi = create(DatasourceSchema, {
  name: "openfigi",
  enabled: true,
  precedence: 1,
  endpoint: "http://stub",
  defaultEndpoint: "https://api.stub",
  credential: "key-1",
  config: { plan: "basic" },
});
const other = create(DatasourceSchema, {
  name: "other",
  precedence: 2,
  defaultEndpoint: "https://api.other",
});
const bare = create(DatasourceSchema, { name: "bare", precedence: 3 });

function serving(updateDatasource = vi.fn()) {
  updateDatasource.mockReturnValue(
    create(UpdateDatasourceResponseSchema, { datasource: openfigi }),
  );
  return transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
    service(DatasourceService, {
      listDatasources: () =>
        create(ListDatasourcesResponseSchema, {
          datasources: [openfigi, other, bare],
        }),
      updateDatasource,
    });
  });
}

describe("DatasourcesPage", () => {
  it("lists the datasources in precedence order with their settings", async () => {
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
    expect(screen.getByTestId("datasource-endpoint-openfigi").textContent).toBe(
      "http://stub",
    );
    expect(
      screen.getByTestId("datasource-credential-openfigi").textContent,
    ).toBe("key-1");
    expect(screen.getByTestId("datasource-grip-openfigi")).toBeTruthy();
    expect(screen.getByTestId("datasource-credential-other").textContent).toBe(
      "none",
    );
  });

  it("shows the provider's default where the row leaves the endpoint empty", async () => {
    renderWithAuth(<DatasourcesPage />, serving());
    await waitFor(() =>
      expect(screen.getByTestId("datasource-row-other")).toBeTruthy(),
    );
    const other = screen.getByTestId("datasource-endpoint-other");
    expect(other.textContent).toBe("https://api.other");
    expect(screen.getByTestId("datasource-endpoint-bare").textContent).toBe("");
  });

  it("toggles a datasource's state, sending its settings as held", async () => {
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
          credential: "key-1",
          config: { plan: "basic" },
        }),
        expect.anything(),
      ),
    );
  });

  it("prefills the endpoint and the credential, and sends the ones edited", async () => {
    const updateDatasource = vi.fn();
    renderWithAuth(<DatasourcesPage />, serving(updateDatasource));
    await waitFor(() =>
      expect(screen.getByTestId("datasource-edit-openfigi")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-edit-openfigi"));
    expect(
      (screen.getByTestId("datasource-endpoint") as HTMLInputElement).value,
    ).toBe("http://stub");
    expect(
      (screen.getByTestId("datasource-credential") as HTMLInputElement).value,
    ).toBe("key-1");
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

  it("prefills the config and sends the one edited", async () => {
    const updateDatasource = vi.fn();
    renderWithAuth(<DatasourcesPage />, serving(updateDatasource));
    await waitFor(() =>
      expect(screen.getByTestId("datasource-edit-openfigi")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-edit-openfigi"));
    const field = screen.getByTestId(
      "datasource-config",
    ) as HTMLTextAreaElement;
    expect(JSON.parse(field.value)).toEqual({ plan: "basic" });
    fireEvent.change(field, { target: { value: '{"plan": "starter"}' } });
    fireEvent.click(screen.getByTestId("datasource-save"));
    await waitFor(() =>
      expect(updateDatasource).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "openfigi",
          config: { plan: "starter" },
        }),
        expect.anything(),
      ),
    );
  });

  it.each(["{", "[]", "1"])(
    "refuses a config of %s without sending it",
    async (text) => {
      const updateDatasource = vi.fn();
      renderWithAuth(<DatasourcesPage />, serving(updateDatasource));
      await waitFor(() =>
        expect(screen.getByTestId("datasource-edit-openfigi")).toBeTruthy(),
      );
      fireEvent.click(screen.getByTestId("datasource-edit-openfigi"));
      fireEvent.change(screen.getByTestId("datasource-config"), {
        target: { value: text },
      });
      fireEvent.click(screen.getByTestId("datasource-save"));
      expect(screen.getByTestId("datasource-config-error")).toBeTruthy();
      expect(screen.getByTestId("datasource-dialog")).toBeTruthy();
      expect(updateDatasource).not.toHaveBeenCalled();
    },
  );

  it("clears the credential when the field is emptied", async () => {
    const updateDatasource = vi.fn();
    renderWithAuth(<DatasourcesPage />, serving(updateDatasource));
    await waitFor(() =>
      expect(screen.getByTestId("datasource-edit-openfigi")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("datasource-edit-openfigi"));
    fireEvent.change(screen.getByTestId("datasource-credential"), {
      target: { value: "" },
    });
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
