"use client";

import Image from "next/image";
import Link from "next/link";
import { useActivity } from "@/contexts/activity-context";
import { useAuth } from "@/contexts/auth-context";
import { Role } from "@/gen/auth/v1/auth_pb";
import { useScheme } from "@/hooks/use-scheme";
import type { Scheme } from "@/lib/scheme";
import { ActivityIcon } from "./activity-icon";
import { ActivitySheet } from "./activity-sheet";
import { Menu, MenuHeader, MenuItem, MenuLink, MenuSeparator } from "./menu";

const schemes: { scheme: Scheme; label: string }[] = [
  { scheme: "light", label: "Light" },
  { scheme: "dark", label: "Dark" },
  { scheme: "system", label: "System" },
];

// TopBar is the same on every page. The logo and wordmark lead home; the
// right holds the activity icon and the profile menu for a user, and a link
// to the sign-in for a visitor.
export function TopBar() {
  const { state, signOut } = useAuth();
  const [scheme, setScheme] = useScheme();
  const activity = useActivity();

  return (
    <header
      data-testid="app-header"
      className="hatch relative z-40 flex h-(--top-bar-height) items-center justify-between gap-4 bg-primary-dark px-4 text-on-dark"
    >
      <Link
        href="/"
        className="flex items-center gap-2.5 transition-opacity hover:opacity-90"
      >
        <Image
          src="/logo-inverted.png"
          alt=""
          width={36}
          height={36}
          unoptimized
          className="h-9 w-9 object-contain"
        />
        <span className="font-display text-lg font-bold tracking-tight">
          Stonks
        </span>
      </Link>
      <div data-testid="user-area" className="flex items-center gap-2">
        {state.status === "unauthenticated" && (
          <Link
            data-testid="top-bar-sign-in"
            href="/"
            className="rounded-lg px-3 py-1.5 text-sm font-medium text-on-dark/90 transition-colors hover:bg-on-dark/15"
          >
            Sign in
          </Link>
        )}
        {state.status === "authenticated" && (
          <>
            <ActivityIcon count={activity.badge} onClick={activity.toggle} />
            <ActivitySheet />
            <Menu
              label={state.user.email}
              testId="user-email"
              panelTestId="profile-menu"
              className="text-on-dark/90 hover:bg-on-dark/15"
            >
              <MenuHeader>{state.user.email}</MenuHeader>
              <MenuLink href="/profile" testId="menu-profile">
                Profile
              </MenuLink>
              <MenuLink href="/statements" testId="menu-statements">
                Statements
              </MenuLink>
              {state.user.role === Role.ADMIN && (
                <MenuLink href="/admin" testId="menu-admin">
                  Admin
                </MenuLink>
              )}
              <MenuSeparator />
              {schemes.map((s) => (
                <MenuItem
                  key={s.scheme}
                  testId={`scheme-${s.scheme}`}
                  selected={scheme === s.scheme}
                  onSelect={() => setScheme(s.scheme)}
                >
                  {s.label}
                </MenuItem>
              ))}
              <MenuSeparator />
              <MenuItem testId="sign-out" onSelect={() => signOut.mutate()}>
                Sign out
              </MenuItem>
            </Menu>
          </>
        )}
      </div>
      <span
        aria-hidden
        className="absolute inset-x-0 bottom-0 h-[3px] bg-linear-to-r from-accent to-primary"
      />
    </header>
  );
}
