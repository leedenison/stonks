import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useDropTarget } from "./use-drop-target";

function Zone({ onFile }: { onFile: (file: File) => void }) {
  return <div data-testid="zone" {...useDropTarget(onFile)} />;
}

describe("useDropTarget", () => {
  it("marks the element while a drag is over it", () => {
    render(<Zone onFile={() => {}} />);
    const zone = screen.getByTestId("zone");
    expect(zone.hasAttribute("data-over")).toBe(false);
    fireEvent.dragOver(zone);
    expect(zone.getAttribute("data-over")).toBe("true");
    fireEvent.dragLeave(zone);
    expect(zone.hasAttribute("data-over")).toBe(false);
  });

  it("hands over the dropped file and clears the mark", () => {
    const onFile = vi.fn();
    render(<Zone onFile={onFile} />);
    const zone = screen.getByTestId("zone");
    const file = new File(["x"], "export.csv", { type: "text/csv" });
    fireEvent.dragOver(zone);
    fireEvent.drop(zone, { dataTransfer: { files: [file] } });
    expect(onFile).toHaveBeenCalledWith(file);
    expect(zone.hasAttribute("data-over")).toBe(false);
  });

  it("ignores a drop without a file", () => {
    const onFile = vi.fn();
    render(<Zone onFile={onFile} />);
    fireEvent.drop(screen.getByTestId("zone"), { dataTransfer: { files: [] } });
    expect(onFile).not.toHaveBeenCalled();
  });
});
