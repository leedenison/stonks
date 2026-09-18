import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { BackButton } from "./back-button";

const router = { back: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router }));

describe("BackButton", () => {
  beforeEach(() => {
    router.back.mockReset();
    router.push.mockReset();
  });

  it("goes back when there is a previous screen", () => {
    window.history.pushState({}, "", "/somewhere");
    render(<BackButton fallback="/x" />);
    fireEvent.click(screen.getByTestId("page-back"));
    expect(router.back).toHaveBeenCalledTimes(1);
    expect(router.push).not.toHaveBeenCalled();
  });

  it("goes to the fallback when opened directly", () => {
    const length = vi.spyOn(window.history, "length", "get").mockReturnValue(1);
    render(<BackButton fallback="/x" />);
    fireEvent.click(screen.getByTestId("page-back"));
    expect(router.push).toHaveBeenCalledWith("/x");
    expect(router.back).not.toHaveBeenCalled();
    length.mockRestore();
  });
});
