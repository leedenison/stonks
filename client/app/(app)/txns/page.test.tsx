import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { UploadProvider, useUpload } from "@/contexts/upload-context";
import TxnsPage from "./page";

// Probe shows what the upload context holds.
function Probe() {
  const { state } = useUpload();
  return (
    <p data-testid="probe">
      {state.open ? "open" : "closed"} {state.file?.name ?? ""}
    </p>
  );
}

describe("TxnsPage", () => {
  it("opens the upload from the action bar and from the empty state", () => {
    render(
      <UploadProvider>
        <TxnsPage />
        <Probe />
      </UploadProvider>,
    );
    expect(screen.getByTestId("probe").textContent).toBe("closed ");
    fireEvent.click(screen.getByTestId("upload-statement"));
    expect(screen.getByTestId("probe").textContent).toBe("open ");
    fireEvent.click(screen.getByTestId("upload-statement-empty"));
    expect(screen.getByTestId("probe").textContent).toBe("open ");
  });

  it("opens the upload with a dropped file", () => {
    render(
      <UploadProvider>
        <TxnsPage />
        <Probe />
      </UploadProvider>,
    );
    const file = new File(["x"], "export.csv", { type: "text/csv" });
    const target = screen.getByTestId("txns-drop");
    fireEvent.dragOver(target, { dataTransfer: { files: [file] } });
    expect(target.hasAttribute("data-over")).toBe(true);
    fireEvent.drop(target, { dataTransfer: { files: [file] } });
    expect(target.hasAttribute("data-over")).toBe(false);
    expect(screen.getByTestId("probe").textContent).toBe("open export.csv");
  });
});
