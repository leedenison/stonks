"use client";

import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useModal } from "@/hooks/use-modal";

// Dialog is a native <dialog> centred over the page: a titled header with
// the close control, its content, which scrolls when tall, and a footer
// that stays put for the actions. It closes on Escape, on its close control
// and on a click outside.
export function Dialog({
  open,
  onClose,
  title,
  testId,
  footer,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  testId: string;
  footer?: ReactNode;
  children: ReactNode;
}) {
  const { ref, onCancel, onClick } = useModal(open, onClose);
  return (
    <dialog
      ref={ref}
      data-testid={testId}
      onCancel={onCancel}
      onClick={onClick}
      className="m-auto w-full max-w-lg rounded-lg bg-surface p-0 text-text-primary shadow-xl backdrop:bg-primary-dark/60 open:flex open:flex-col"
    >
      <DialogHeader title={title} testId={testId} onClose={onClose} />
      <div className="flex max-h-[70dvh] flex-col gap-4 overflow-y-auto px-5 py-4">
        {children}
      </div>
      {footer && (
        <div className="flex justify-between gap-3 border-t border-border px-5 py-3">
          {footer}
        </div>
      )}
    </dialog>
  );
}

// DialogHeader is the titled bar of a dialog or a sheet, with the close
// control the owner's onClose answers.
export function DialogHeader({
  title,
  testId,
  onClose,
}: {
  title: string;
  testId: string;
  onClose: () => void;
}) {
  return (
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
  );
}
