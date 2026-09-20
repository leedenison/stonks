"use client";

import { type MouseEvent, type SyntheticEvent, useEffect, useRef } from "react";

// useModal keeps a <dialog> open while open is true. A modal one is shown
// through showModal, so the element traps focus, paints its backdrop and
// raises cancel on Escape. One that is not modal is shown beside the page
// with focus moved to its first button, and Escape reaches it through a
// document listener, since only a modal dialog raises cancel. Escape and a
// click on the backdrop ask the owner to close rather than closing the
// element, so the owner's state stays the one truth.
export function useModal(
  open: boolean,
  onClose: () => void,
  { modal = true }: { modal?: boolean } = {},
) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) {
      return;
    }
    if (open && !el.open) {
      if (modal) {
        el.showModal();
      } else {
        el.show();
        el.querySelector("button")?.focus();
      }
    } else if (!open && el.open) {
      el.close();
    }
  }, [open, modal]);

  useEffect(() => {
    if (!open || modal) {
      return;
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, modal, onClose]);

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
