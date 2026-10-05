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

// Pager links to the newest page and the next older page, each only when
// the listing has one.
export function Pager({
  newest,
  older,
}: {
  newest: string | undefined;
  older: string | undefined;
}) {
  if (!newest && !older) return null;
  return (
    <div className="flex gap-2">
      {newest && (
        <LinkButton variant="secondary" href={newest}>
          Newest
        </LinkButton>
      )}
      {older && (
        <LinkButton variant="secondary" data-testid="list-older" href={older}>
          Older
        </LinkButton>
      )}
    </div>
  );
}
