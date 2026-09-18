"use client";

import { ArrowLeft } from "lucide-react";
import { useRouter } from "next/navigation";

// BackButton returns to the previous screen. A page opened directly has no
// previous screen in this tab, and then it goes to fallback, the page a user
// would have come from.
export function BackButton({ fallback }: { fallback: string }) {
  const router = useRouter();
  return (
    <button
      type="button"
      data-testid="page-back"
      aria-label="Back"
      onClick={() => {
        if (window.history.length > 1) {
          router.back();
        } else {
          router.push(fallback);
        }
      }}
      className="rounded-md p-1 text-text-muted transition-colors hover:bg-primary-light/15 hover:text-text-primary"
    >
      <ArrowLeft aria-hidden className="h-5 w-5" />
    </button>
  );
}
