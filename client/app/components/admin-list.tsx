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

// Pager leads to the newest page and to the next older one, where they
// differ from the page shown.
export function Pager({
  path,
  p,
  next,
}: {
  path: string;
  p: ListParams;
  next: string | undefined;
}) {
  if (!p.before && !next) return null;
  return (
    <div className="flex gap-2">
      {p.before && (
        <LinkButton
          variant="secondary"
          href={listQuery(path, { ...p, before: "" })}
        >
          Newest
        </LinkButton>
      )}
      {next && (
        <LinkButton
          variant="secondary"
          data-testid="list-older"
          href={listQuery(path, { ...p, before: next })}
        >
          Older
        </LinkButton>
      )}
    </div>
  );
}
