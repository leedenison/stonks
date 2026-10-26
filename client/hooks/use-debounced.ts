import { useEffect, useState } from "react";

// useDebounced returns value once it has held still for delay milliseconds,
// so a search box asks for results after the last keystroke.
export function useDebounced<T>(value: T, delay: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setSettled(value), delay);
    return () => clearTimeout(t);
  }, [value, delay]);
  return settled;
}
