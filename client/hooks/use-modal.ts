"use client";

import { type MouseEvent, type SyntheticEvent, useEffect, useRef } from "react";

// useModal keeps a <dialog> open while open is true, through showModal so
// the element traps focus and paints its backdrop. Escape and a click on
// the backdrop ask the owner to close rather than closing the element, so
// the owner's state stays the one truth.
export function useModal(open: boolean, onClose: () => void) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) {
      return;
    }
    if (open && !el.open) {
      el.showModal();
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);

  const onCancel = (e: SyntheticEvent<HTMLDialogElement>) => {
    e.preventDefault();
    onClose();
  };
  const onClick = (e: MouseEvent<HTMLDialogElement>) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };
  return { ref, onCancel, onClick };
}
