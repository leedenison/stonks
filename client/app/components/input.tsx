import type { ComponentProps } from "react";

// The class names of a form field, shared by text inputs and selects so both
// show the same focus ring.
const fieldClass =
  "rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-text-primary focus:border-primary focus:ring-1 focus:ring-primary/30 focus:outline-hidden";

export function Input({ className = "", ...rest }: ComponentProps<"input">) {
  return <input className={`${fieldClass} ${className}`} {...rest} />;
}

export function Select({ className = "", ...rest }: ComponentProps<"select">) {
  return <select className={`${fieldClass} ${className}`} {...rest} />;
}
