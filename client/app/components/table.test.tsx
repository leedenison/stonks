import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Td, Tr } from "./table";

const router = { push: vi.fn() };

vi.mock("next/navigation", () => ({ useRouter: () => router }));

describe("Tr", () => {
  it("opens its page from a click on the row, leaving a control within it to act alone", () => {
    render(
      <table>
        <tbody>
          <Tr href="/somewhere" data-testid="row">
            <Td data-testid="cell">text</Td>
            <Td>
              <button type="button" data-testid="button">
                act
              </button>
            </Td>
          </Tr>
        </tbody>
      </table>,
    );
    fireEvent.click(screen.getByTestId("button"));
    expect(router.push).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("cell"));
    expect(router.push).toHaveBeenCalledWith("/somewhere");
  });
});
