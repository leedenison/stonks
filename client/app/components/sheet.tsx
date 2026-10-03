"use client";

import type { ReactNode } from "react";
import { useModal } from "@/hooks/use-modal";
import { DialogHeader } from "./dialog";

// Sheet is a panel anchored to the right edge of the page, under the top
// bar. It is not modal, so the top bar stays live and its icon can
// toggle the sheet closed. A click on the overlay, Escape or the close
// control asks the owner to close.
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
  const { ref } = useModal(open, onClose, { modal: false });

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
        <DialogHeader title={title} testId={testId} onClose={onClose} />
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-5 py-4">
          {children}
        </div>
      </dialog>
    </>
  );
}
