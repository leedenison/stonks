import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SkeletonRows } from "./skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "./table";

const router = { push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router }));

describe("TableCard", () => {
  it("renders a table whose row with an href navigates on click", () => {
    render(
      <TableCard testId="t">
        <Thead>
          <tr>
            <Th>Name</Th>
            <Th numeric>Count</Th>
          </tr>
        </Thead>
        <tbody>
          <Tr href="/things/1" data-testid="row">
            <Td>One</Td>
            <Td numeric>1</Td>
          </Tr>
        </tbody>
      </TableCard>,
    );
    expect(screen.getByTestId("t").tagName).toBe("TABLE");
    fireEvent.click(screen.getByTestId("row"));
    expect(router.push).toHaveBeenCalledWith("/things/1");
  });

  it("fills the body with skeleton rows while loading", () => {
    render(
      <TableCard>
        <SkeletonRows columns={3} rows={2} />
      </TableCard>,
    );
    const body = screen.getByTestId("skeleton-rows");
    expect(body.querySelectorAll("tr").length).toBe(2);
    expect(body.querySelectorAll("td").length).toBe(6);
  });
});
