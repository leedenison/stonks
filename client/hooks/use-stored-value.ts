"use client";

import { useCallback, useSyncExternalStore } from "react";

// listeners hears a write in this tab. A write in another tab arrives as a
// storage event.
const listeners = new Set<() => void>();

function subscribe(cb: () => void) {
  listeners.add(cb);
  window.addEventListener("storage", cb);
  return () => {
    listeners.delete(cb);
    window.removeEventListener("storage", cb);
  };
}

// readStored returns null when storage is absent or refuses access, as in a
// private window or a sandbox.
export function readStored(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writeStored(key: string, value: string | null) {
  try {
    if (value === null) {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, value);
    }
  } catch {
    // Storage refused the write; the fallback stands.
  }
  listeners.forEach((cb) => cb());
}

// useStoredValue keeps a value in localStorage under key. The server render
// and the first client render both see nothing stored, so they agree. The
// stored value paints on the render after.
export function useStoredValue<T>(
  key: string,
  parse: (raw: string | null) => T,
): [T, (value: string | null) => void] {
  const raw = useSyncExternalStore(
    subscribe,
    () => readStored(key),
    () => null,
  );
  const set = useCallback(
    (value: string | null) => writeStored(key, value),
    [key],
  );
  return [parse(raw), set];
}
