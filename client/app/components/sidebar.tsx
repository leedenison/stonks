"use client";

import {
  ArrowLeftRight,
  PanelLeftClose,
  PanelLeftOpen,
  Wallet,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { useStoredValue } from "@/hooks/use-stored-value";

const items = [
  {
    href: "/holdings",
    label: "Holdings",
    icon: Wallet,
    testId: "nav-holdings",
  },
  {
    href: "/transactions",
    label: "Transactions",
    icon: ArrowLeftRight,
    testId: "nav-transactions",
  },
];

export const sidebarKey = "stonks.sidebar";

// Sidebar lists the user pages, with the primary action above them. It is a
// rail of icons below the lg breakpoint, and the user can collapse it to
// the rail by hand at any width; that choice is kept in localStorage.
export function Sidebar({ actions }: { actions?: ReactNode }) {
  const pathname = usePathname();
  const [collapsed, set] = useStoredValue(
    sidebarKey,
    (raw) => raw === "collapsed",
  );
  const label = collapsed ? "sr-only" : "sr-only lg:not-sr-only";

  return (
    <aside
      data-testid="sidebar"
      data-collapsed={collapsed ? "" : undefined}
      className={`flex shrink-0 flex-col gap-4 border-r border-border bg-surface px-2 py-4 ${collapsed ? "w-14" : "w-14 lg:w-52 lg:px-3"}`}
    >
      {actions && <div className="flex flex-col">{actions}</div>}
      <nav className="flex flex-col gap-1">
        {items.map(({ href, label: text, icon: Icon, testId }) => {
          const active = pathname === href || pathname.startsWith(href + "/");
          return (
            <Link
              key={href}
              href={href}
              data-testid={testId}
              aria-current={active ? "page" : undefined}
              title={text}
              className={`relative flex items-center gap-3 rounded-md px-3 py-2.5 text-sm font-medium tracking-wide transition-colors ${
                active
                  ? "bg-primary-dark/5 font-semibold text-primary-dark before:absolute before:top-1 before:bottom-1 before:left-0 before:w-[3px] before:rounded-full before:bg-accent dark:text-primary"
                  : "text-text-muted hover:bg-primary-light/15 hover:text-text-primary"
              }`}
            >
              <Icon aria-hidden className="h-4 w-4 shrink-0" />
              <span className={label}>{text}</span>
            </Link>
          );
        })}
      </nav>
      <button
        type="button"
        data-testid="sidebar-collapse"
        aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
        aria-expanded={!collapsed}
        onClick={() => set(collapsed ? null : "collapsed")}
        className="mt-auto hidden w-fit rounded-md p-2 text-text-muted transition-colors hover:bg-primary-light/15 hover:text-text-primary lg:flex"
      >
        {collapsed ? (
          <PanelLeftOpen aria-hidden className="h-4 w-4" />
        ) : (
          <PanelLeftClose aria-hidden className="h-4 w-4" />
        )}
      </button>
    </aside>
  );
}
