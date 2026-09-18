"use client";

import { useEffect } from "react";
import { applyScheme, parseScheme, type Scheme, schemeKey } from "@/lib/scheme";
import { useStoredValue } from "./use-stored-value";

// The chosen scheme and its setter. The attribute on <html> follows the
// stored value, so a choice made in another tab applies here as well.
export function useScheme(): [Scheme, (scheme: Scheme) => void] {
  const [scheme, set] = useStoredValue(schemeKey, parseScheme);
  useEffect(() => {
    applyScheme(document.documentElement, scheme);
  }, [scheme]);
  return [scheme, (next) => set(next === "system" ? null : next)];
}
