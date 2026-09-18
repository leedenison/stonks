"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

type Entry = { href: string; label: string; disabled?: boolean };
type Section = { section: string; children: Entry[] };

// The admin pages, in the shape they take once built. An entry that is not
// yet built is listed dimmed and inert, so the navigation does not change
// when it arrives.
const entries: (Entry | Section)[] = [
  { href: "/admin", label: "Overview" },
  {
    section: "Runs",
    children: [{ href: "/admin/runs", label: "Runs", disabled: true }],
  },
  {
    section: "Findings",
    children: [{ href: "/admin/findings", label: "Findings", disabled: true }],
  },
  {
    section: "Data",
    children: [
      { href: "/admin/datasources", label: "Datasources", disabled: true },
    ],
  },
];

function isSection(e: Entry | Section): e is Section {
  return "section" in e;
}

function Item({
  entry,
  active,
  className,
}: {
  entry: Entry;
  active: boolean;
  className: string;
}) {
  if (entry.disabled) {
    return (
      <span
        aria-disabled="true"
        className={`${className} cursor-default text-text-muted opacity-40`}
      >
        {entry.label}
      </span>
    );
  }
  return (
    <Link
      href={entry.href}
      aria-current={active ? "page" : undefined}
      className={`${className} transition-colors ${
        active
          ? "font-semibold text-primary-dark dark:text-primary"
          : "text-text-muted hover:text-primary"
      }`}
    >
      {entry.label}
    </Link>
  );
}

export function AdminNav() {
  const pathname = usePathname();
  const activeAt = (href: string) =>
    href === "/admin"
      ? pathname === href
      : pathname === href || pathname.startsWith(href + "/");

  return (
    <nav
      data-testid="admin-nav"
      className="w-56 shrink-0 border-r border-border bg-surface px-6 py-6"
    >
      <ul className="space-y-2">
        {entries.map((entry) =>
          isSection(entry) ? (
            <li key={entry.section}>
              <span
                className={`block py-1 pl-3 text-xs font-semibold tracking-wider uppercase ${
                  entry.children.some((c) => !c.disabled && activeAt(c.href))
                    ? "text-primary-dark dark:text-primary"
                    : "text-text-muted"
                }`}
              >
                {entry.section}
              </span>
              <ul className="mt-1 ml-3 space-y-0.5 border-l-2 border-border pl-3">
                {entry.children.map((child) => (
                  <li key={child.href}>
                    <Item
                      entry={child}
                      active={activeAt(child.href)}
                      className="block py-0.5 text-sm"
                    />
                  </li>
                ))}
              </ul>
            </li>
          ) : (
            <li key={entry.href}>
              <Item
                entry={entry}
                active={activeAt(entry.href)}
                className={`relative block py-1 pl-3 text-sm ${
                  activeAt(entry.href)
                    ? "before:absolute before:top-0 before:bottom-0 before:left-0 before:w-[3px] before:rounded-full before:bg-accent"
                    : ""
                }`}
              />
            </li>
          ),
        )}
      </ul>
    </nav>
  );
}
