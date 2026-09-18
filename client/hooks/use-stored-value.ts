"use client";

import { useCallback, useSyncExternalStore } from "react";

// A value kept in localStorage. It is read through useSyncExternalStore so
// the server render and the first client render agree on the fallback and
// the stored value paints on the next. Storage can be absent or refuse
// access, in a private window or a sandbox; the value is then the fallback
// and a write is dropped. A write in this tab notifies every hook holding a
// key; a write in another tab arrives as a storage event.

const listeners = new Set<() => void>();

function subscribe(cb: () => void) {
  listeners.add(cb);
  window.addEventListener("storage", cb);
  return () => {
    listeners.delete(cb);
    window.removeEventListener("storage", cb);
  };
}

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
