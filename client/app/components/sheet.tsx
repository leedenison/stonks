"use client";

import { X } from "lucide-react";
import { type ReactNode, useEffect, useRef } from "react";

// Sheet is a native <dialog> anchored to the right edge under the top bar,
// full height, over the page but not modal: the top bar stays live, so its
// icon can toggle the sheet closed. An overlay covers the page below the
// bar, and a click on it, Escape, or the close control asks the owner to
// close.
export function Sheet({
  open,
  onClose,
  title,
  testId,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  testId: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) {
      return;
    }
    if (open && !el.open) {
      el.show();
      el.querySelector("button")?.focus();
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  return (
    <>
      {open && (
        <div
          data-testid={`${testId}-overlay`}
          onClick={onClose}
          className="fixed inset-x-0 top-(--top-bar-height) bottom-0 z-30 bg-primary-dark/40"
        />
      )}
      <dialog
        ref={ref}
        data-testid={testId}
        className="fixed inset-auto top-(--top-bar-height) right-0 bottom-0 z-30 m-0 h-[calc(100dvh-var(--top-bar-height))] max-h-none w-full max-w-md flex-col border-l border-border bg-surface p-0 text-text-primary shadow-xl open:flex"
      >
        <div className="flex items-center justify-between border-b border-border px-5 py-3">
          <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
          <button
            type="button"
            aria-label="Close"
            data-testid={`${testId}-close`}
            onClick={onClose}
            className="rounded-md p-1 text-text-muted transition-colors hover:bg-primary-light/15 hover:text-text-primary"
          >
            <X aria-hidden className="h-5 w-5" />
          </button>
        </div>
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-5 py-4">
          {children}
        </div>
      </dialog>
    </>
  );
}
