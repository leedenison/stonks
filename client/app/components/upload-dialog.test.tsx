import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createRouterTransport,
  type ServiceImpl,
} from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import {
  type CreateStatementRequest,
  CreateStatementResponseSchema,
  StatementService,
} from "@/gen/statement/v1/statement_pb";
import { Broker } from "@/gen/type/v1/type_pb";
import { fixture } from "@/lib/marshal/test-utils";
import { renderWithAuth } from "@/lib/test-utils";
import { UploadDialog } from "./upload-dialog";

const live = create(GetSessionResponseSchema, {
  user: create(UserSchema, {
    id: "u1",
    email: "a@example.com",
    role: Role.USER,
  }),
  session: create(SessionSchema, {
    expiresAt: timestampFromDate(new Date("2026-09-16T00:00:00Z")),
  }),
});

const pending = create(CreateStatementResponseSchema, {
  run: create(RunSchema, { id: "r1", state: RunState.PENDING }),
});

function transportWith(
  createStatement: ServiceImpl<typeof StatementService>["createStatement"],
) {
  return createRouterTransport(({ service }) => {
    service(AuthService, { getSession: () => live });
    service(StatementService, { createStatement });
  });
}

function choose(name: string, type: string, text: string) {
  const file = new File([text], name, { type });
  fireEvent.change(screen.getByTestId("upload-file"), {
    target: { files: [file] },
  });
}

const select = () => screen.getByTestId("upload-broker") as HTMLSelectElement;
const input = (id: string) => screen.getByTestId(id) as HTMLInputElement;

describe("UploadDialog", () => {
  it("recognises a Fidelity UK export and reviews it", async () => {
    renderWithAuth(
      <UploadDialog open onClose={() => {}} />,
      transportWith(() => pending),
    );
    choose("activity.csv", "text/csv", fixture("fidelity-uk.csv"));
    await waitFor(() =>
      expect(select().value).toBe(String(Broker.FIDELITY_UK)),
    );
    expect(screen.getByTestId("upload-rows").textContent).toContain("11 rows");
    expect(screen.getByTestId("upload-recognised").textContent).toBe(
      "activity.csv - Recognised as: Fidelity UK export",
    );
    expect(input("upload-from").value).toBe("2025-01-01");
    expect(input("upload-to").value).toBe("2025-03-16");
    expect(screen.queryByTestId("upload-exported-on")).toBeNull();
    expect(
      screen.getByTestId("upload-preview").querySelectorAll("tbody tr").length,
    ).toBe(5);
    expect(screen.queryByTestId("upload-outside")).toBeNull();
  });

  it("asks a Schwab export for the date it was taken and re-parses on change", async () => {
    const createStatement = vi.fn<
      (req: CreateStatementRequest) => typeof pending
    >(() => pending);
    renderWithAuth(
      <UploadDialog open onClose={() => {}} />,
      transportWith(createStatement),
    );
    choose("history.csv", "text/csv", fixture("schwab.csv"));
    await waitFor(() => expect(select().value).toBe(String(Broker.SCHWAB)));
    fireEvent.change(input("upload-exported-on"), {
      target: { value: "2025-01-15" },
    });
    fireEvent.click(screen.getByTestId("upload-submit"));
    await waitFor(() => expect(createStatement).toHaveBeenCalledTimes(1));
    const sent = createStatement.mock.calls[0]?.[0].statement;
    expect(sent?.rows[0]?.asAt).toBe("2025-01-15");
  });

  it("narrows the period, warns about the rows outside it and submits it", async () => {
    const createStatement = vi.fn<
      (req: CreateStatementRequest) => typeof pending
    >(() => pending);
    const onClose = vi.fn();
    const onCreated = vi.fn();
    renderWithAuth(
      <UploadDialog open onClose={onClose} onCreated={onCreated} />,
      transportWith(createStatement),
    );
    choose("activity.csv", "text/csv", fixture("fidelity-uk.csv"));
    await waitFor(() => expect(input("upload-from").value).toBe("2025-01-01"));
    fireEvent.change(input("upload-from"), { target: { value: "2025-02-01" } });
    expect(screen.getByTestId("upload-outside").textContent).toMatch(
      /^\d+ of the rows fall outside/,
    );
    fireEvent.click(screen.getByTestId("upload-submit"));
    await waitFor(() => expect(createStatement).toHaveBeenCalledTimes(1));
    const sent = createStatement.mock.calls[0]?.[0].statement;
    expect(sent?.orderFrom).toBe("2025-02-01");
    expect(sent?.orderBefore).toBe("2025-03-17");
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(onCreated).toHaveBeenCalledWith(pending.run);
  });

  it("shows why a file cannot be marshalled and offers another", async () => {
    renderWithAuth(
      <UploadDialog open onClose={() => {}} />,
      transportWith(() => pending),
    );
    choose("notes.txt", "text/plain", "hello,world\n1,2\n");
    await waitFor(() => expect(select()).toBeTruthy());
    expect(select().value).toBe("");
    expect(screen.getByTestId("upload-recognised").textContent).toBe(
      "notes.txt - Not recognised: choose the broker",
    );
    expect(
      screen.queryByTestId("upload-submit")?.hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.change(select(), { target: { value: String(Broker.IBKR) } });
    await waitFor(() =>
      expect(screen.getByTestId("upload-error")).toBeTruthy(),
    );
    fireEvent.click(screen.getByTestId("upload-back"));
    expect(screen.getByTestId("upload-file")).toBeTruthy();
  });

  it("refuses a file larger than an export", async () => {
    renderWithAuth(
      <UploadDialog open onClose={() => {}} />,
      transportWith(() => pending),
    );
    const big = new File([new Uint8Array(5 * 1024 * 1024 + 1)], "big.csv", {
      type: "text/csv",
    });
    const text = vi.spyOn(big, "text");
    fireEvent.change(screen.getByTestId("upload-file"), {
      target: { files: [big] },
    });
    await waitFor(() =>
      expect(screen.getByTestId("upload-error")).toBeTruthy(),
    );
    expect(screen.getByTestId("upload-file")).toBeTruthy();
    expect(text).not.toHaveBeenCalled();
  });

  it("reports a file that cannot be read and offers another", async () => {
    renderWithAuth(
      <UploadDialog open onClose={() => {}} />,
      transportWith(() => pending),
    );
    const gone = new File(["x"], "gone.csv", { type: "text/csv" });
    vi.spyOn(gone, "text").mockRejectedValue(
      new DOMException("moved", "NotReadableError"),
    );
    fireEvent.change(screen.getByTestId("upload-file"), {
      target: { files: [gone] },
    });
    await waitFor(() =>
      expect(screen.getByTestId("upload-error").textContent).toBe(
        "gone.csv could not be read.",
      ),
    );
    expect(screen.getByTestId("upload-file")).toBeTruthy();
  });

  it("shows the service's refusal and stays open", async () => {
    const onClose = vi.fn();
    renderWithAuth(
      <UploadDialog open onClose={onClose} />,
      transportWith(() => {
        throw new ConnectError(
          "order_from must precede order_before",
          Code.InvalidArgument,
        );
      }),
    );
    choose("activity.csv", "text/csv", fixture("fidelity-uk.csv"));
    await waitFor(() => expect(input("upload-from").value).toBe("2025-01-01"));
    fireEvent.click(screen.getByTestId("upload-submit"));
    await waitFor(() =>
      expect(screen.getByTestId("upload-refused").textContent).toContain(
        "order_from must precede",
      ),
    );
    expect(onClose).not.toHaveBeenCalled();
  });

  it("refuses an oversize file handed over at opening without reading it", async () => {
    const big = new File([new Uint8Array(5 * 1024 * 1024 + 1)], "big.csv", {
      type: "text/csv",
    });
    const text = vi.spyOn(big, "text");
    renderWithAuth(
      <UploadDialog open onClose={() => {}} initial={big} />,
      transportWith(() => pending),
    );
    await waitFor(() =>
      expect(screen.getByTestId("upload-error")).toBeTruthy(),
    );
    expect(screen.getByTestId("upload-file")).toBeTruthy();
    expect(text).not.toHaveBeenCalled();
  });

  it("reads a file handed over at opening", async () => {
    renderWithAuth(
      <UploadDialog
        open
        onClose={() => {}}
        initial={
          new File([fixture("schwab.json")], "h.json", {
            type: "application/json",
          })
        }
      />,
      transportWith(() => pending),
    );
    await waitFor(() => expect(select().value).toBe(String(Broker.SCHWAB)));
  });
});
