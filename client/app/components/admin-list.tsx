"use client";

import { useRouter } from "next/navigation";
import { type ListParams, listQuery } from "@/lib/admin";
import { LinkButton } from "./button";

// ClearedToggle includes or leaves out the cleared rows of the listing at
// path, returning to its first page.
export function ClearedToggle({ path, p }: { path: string; p: ListParams }) {
  const router = useRouter();
  return (
    <label className="flex w-fit items-center gap-2 text-sm">
      <input
        type="checkbox"
        data-testid="include-cleared"
        checked={p.cleared}
        onChange={(e) =>
          router.replace(
            listQuery(path, { cleared: e.target.checked, before: "" }),
          )
        }
      />
      <span className="text-text-muted">Include cleared</span>
    </label>
  );
}

// Pager links to the first page and the next page, each only when the
// listing has one. The labels say which way the listing runs.
export function Pager({
  first,
  next,
  labels = { first: "Newest", next: "Older" },
}: {
  first: string | undefined;
  next: string | undefined;
  labels?: { first: string; next: string };
}) {
  if (!first && !next) return null;
  return (
    <div className="flex gap-2">
      {first && (
        <LinkButton variant="secondary" href={first}>
          {labels.first}
        </LinkButton>
      )}
      {next && (
        <LinkButton variant="secondary" data-testid="list-older" href={next}>
          {labels.next}
        </LinkButton>
      )}
    </div>
  );
}
