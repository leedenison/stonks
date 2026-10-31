import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import type { ServiceImpl } from "@connectrpc/connect";
import {
  AlertGroupSchema,
  AlertKind,
  AlertService,
  GetAlertDigestResponseSchema,
} from "@/gen/admin/alert/v1/alert_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import AdminLayout from "./layout";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn() }),
  usePathname: () => "/admin",
}));

describe("AdminLayout", () => {
  it("denies a user", async () => {
    renderWithAuth(
      <AdminLayout>
        <p>admin child</p>
      </AdminLayout>,
      transportWith(liveSession({ role: Role.USER })),
    );
    await waitFor(() =>
      expect(screen.getByTestId("access-denied")).toBeTruthy(),
    );
    expect(screen.queryByText("admin child")).toBeNull();
    expect(screen.queryByTestId("admin-nav")).toBeNull();
  });

  it("shows an administrator the navigation and the page", async () => {
    renderWithAuth(
      <AdminLayout>
        <p>admin child</p>
      </AdminLayout>,
      transportWith(liveSession({ role: Role.ADMIN })),
    );
    await waitFor(() => expect(screen.getByText("admin child")).toBeTruthy());
    expect(screen.getByTestId("admin-nav")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Alerts" }).getAttribute("aria-current"),
    ).toBe("page");
    expect(
      screen.getByRole("link", { name: "Blocks" }).getAttribute("href"),
    ).toBe("/admin/blocks");
    expect(
      screen.getByRole("link", { name: "Instruments" }).getAttribute("href"),
    ).toBe("/admin/instrs");
  });

  it("counts the open alerts beside the Alerts entry", async () => {
    const digest = (counts: number[]) =>
      create(GetAlertDigestResponseSchema, {
        groups: counts.map((count, i) =>
          create(AlertGroupSchema, { kind: AlertKind.FAILED_RUN + i, count }),
        ),
      });
    renderWithAuth(
      <AdminLayout>
        <p>admin child</p>
      </AdminLayout>,
      transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
        service(AlertService, {
          getAlertDigest: () => digest([1, 2, 0, 4]),
        } satisfies Partial<ServiceImpl<typeof AlertService>>);
      }),
    );
    await waitFor(() =>
      expect(screen.getByTestId("admin-nav-alerts-count").textContent).toBe(
        "7",
      ),
    );
  });

  it("hides the count while it loads and at zero", async () => {
    renderWithAuth(
      <AdminLayout>
        <p>admin child</p>
      </AdminLayout>,
      transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
        service(AlertService, {
          getAlertDigest: () => create(GetAlertDigestResponseSchema, {}),
        } satisfies Partial<ServiceImpl<typeof AlertService>>);
      }),
    );
    expect(screen.queryByTestId("admin-nav-alerts-count")).toBeNull();
    await waitFor(() => expect(screen.getByText("admin child")).toBeTruthy());
    expect(screen.queryByTestId("admin-nav-alerts-count")).toBeNull();
  });
});
