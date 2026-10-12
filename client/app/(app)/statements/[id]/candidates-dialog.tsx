"use client";

import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { Dialog } from "@/app/components/dialog";
import { EmptyState } from "@/app/components/empty-state";
import { IdentifierChips } from "@/app/components/identifier-chip";
import { Notice } from "@/app/components/notice";
import { Skeleton } from "@/app/components/skeleton";
import type { Candidate } from "@/gen/statement/v1/statement_pb";
import { AssetClass, type ResolutionItem } from "@/gen/type/v1/type_pb";
import { useCandidates, useChooseCandidate } from "@/hooks/use-candidates";
import { assetClassLabel } from "@/lib/asset-class";
import { refusal } from "@/lib/refusal";

// CandidatesDialog lists the instruments the datasources offer for a key
// and lets the user choose one. Each candidate is one security, with every
// datasource that describes it. Opening it resolves the key again. When
// the service refuses a choice, the dialog shows the refusal and finds the
// candidates again, since the answer may have changed. When a choice lands,
// the dialog closes.
export function CandidatesDialog({
  item,
  onClose,
}: {
  item: ResolutionItem;
  onClose: () => void;
}) {
  const keyId = item.statedKeyId;
  const { data, isPending, error, refetch } = useCandidates(keyId);
  const choose = useChooseCandidate();
  const pick = (c: Candidate) =>
    choose.mutate(
      { statedKeyId: keyId, datasource: c.datasource, identifier: c.strongest },
      { onSuccess: onClose, onError: () => refetch() },
    );

  return (
    <Dialog
      open
      onClose={onClose}
      title="Choose an instrument"
      testId="candidates-dialog"
      footer={
        <Button
          variant="secondary"
          onClick={onClose}
          disabled={choose.isPending}
        >
          Cancel
        </Button>
      }
    >
      {choose.isError && (
        <Notice tone="error" testId="candidates-error">
          {refusal(choose.error, "The instrument could not be chosen.")}
        </Notice>
      )}
      {error && (
        <Notice tone="error" onRetry={() => refetch()}>
          The candidates could not be found.
        </Notice>
      )}
      {!error && isPending && <Skeleton lines={3} />}
      {data && data.candidates.length === 0 && (
        <EmptyState message="No datasource offered an instrument for this key." />
      )}
      {data && data.candidates.length > 0 && (
        <ul className="flex flex-col gap-3">
          {data.candidates.map((c, i) => (
            <CandidateRow
              key={`${candidateId(c)}-${i}`}
              candidate={c}
              busy={choose.isPending}
              onChoose={() => pick(c)}
            />
          ))}
        </ul>
      )}
      {data && data.reasons.length > 0 && (
        <ul
          className="text-sm text-text-muted"
          data-testid="candidates-reasons"
        >
          {data.reasons.map((r, i) => (
            <li key={i}>{r}</li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}

// candidateId names a candidate by its datasource and the identifier that
// names it most firmly, or by its datasource alone where it has none. Two
// candidates may share both, so the list keys each row by its index too.
function candidateId(c: Candidate): string {
  return c.strongest ? `${c.datasource}-${c.strongest.value}` : c.datasource;
}

// CandidateRow is one instrument on offer, with the control that chooses
// it.
function CandidateRow({
  candidate,
  busy,
  onChoose,
}: {
  candidate: Candidate;
  busy: boolean;
  onChoose: () => void;
}) {
  const c = candidate;
  const id = candidateId(c);
  return (
    <li
      data-testid={`candidate-row-${id}`}
      className="flex flex-col gap-2 rounded-md border border-border px-3 py-2 text-sm"
    >
      <div className="flex items-center justify-between gap-3">
        <span className="flex flex-wrap items-center gap-1">
          {[c.datasource, ...c.attached].map((d) => (
            <Chip key={d} data-testid={`candidate-datasource-${d}`}>
              {d}
            </Chip>
          ))}
          {c.assetClass !== AssetClass.UNSPECIFIED && (
            <Chip tone="accent">{assetClassLabel(c.assetClass)}</Chip>
          )}
        </span>
        <Button
          variant="secondary"
          data-testid={`candidate-choose-${id}`}
          disabled={busy}
          onClick={onChoose}
        >
          Choose
        </Button>
      </div>
      {c.identifiers.length > 0 && <IdentifierChips ids={c.identifiers} />}
      {c.listings.map((l, i) => (
        <div key={i} className="flex flex-wrap items-center gap-1">
          <span className="w-16 font-mono text-text-muted">
            {l.currency ?? "none"}
          </span>
          <IdentifierChips ids={l.identifiers} />
        </div>
      ))}
    </li>
  );
}
