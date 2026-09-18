"use client";

import Link from "next/link";
import type { ComponentProps, ReactNode } from "react";

export type Variant = "primary" | "secondary";

// Primary is the one action a view exists for; a view holds at most one.
const styles: Record<Variant, string> = {
  primary: "bg-accent-dark text-on-dark hover:brightness-95",
  secondary:
    "border border-border bg-surface text-text-primary hover:bg-primary-light/15",
};

const base =
  "inline-flex w-fit items-center gap-1.5 rounded-md px-3.5 py-1.5 text-sm font-semibold transition-colors disabled:pointer-events-none disabled:opacity-50";

export function Button({
  variant = "primary",
  className = "",
  type = "button",
  ...rest
}: ComponentProps<"button"> & { variant?: Variant }) {
  return (
    <button
      type={type}
      className={`${base} ${styles[variant]} ${className}`}
      {...rest}
    />
  );
}

export function LinkButton({
  variant = "primary",
  className = "",
  href,
  children,
  ...rest
}: ComponentProps<typeof Link> & { variant?: Variant; children: ReactNode }) {
  return (
    <Link
      href={href}
      className={`${base} ${styles[variant]} ${className}`}
      {...rest}
    >
      {children}
    </Link>
  );
}
