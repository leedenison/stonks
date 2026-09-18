import type { ComponentProps } from "react";

// The tones a chip takes. Muted is a plain categorical value; the others
// carry the meaning their colour has everywhere else.
export type Tone = "muted" | "primary" | "positive" | "accent" | "negative";

const tones: Record<Tone, string> = {
  muted: "bg-text-muted/10 text-text-muted",
  primary: "bg-primary/10 text-primary-dark dark:text-primary",
  positive: "bg-positive/10 text-positive",
  accent: "bg-accent/15 text-on-accent-soft",
  negative: "bg-negative/10 text-negative",
};

export function Chip({
  tone = "muted",
  className = "",
  ...rest
}: ComponentProps<"span"> & { tone?: Tone }) {
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-xs font-medium whitespace-nowrap ${tones[tone]} ${className}`}
      {...rest}
    />
  );
}
