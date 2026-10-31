"use client";

import { useState } from "react";
import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { MarketTickerChip } from "@/app/components/identifier-chip";
import { Select } from "@/app/components/input";
import { Notice } from "@/app/components/notice";
import { Skeleton } from "@/app/components/skeleton";
import {
  type CorpAct,
  CorpActDecision,
  type GetCorpActResponse,
} from "@/gen/admin/corpact/v1/corpact_pb";
import { useCorpAct, useDecideCorpAct } from "@/hooks/use-corp-acts";
import { enumLabel, enumValues } from "@/lib/enum";
import { formatRatio } from "@/lib/format";
import { refusal } from "@/lib/refusal";

// DecideCorpActDialog lets an administrator record the type of a corporate
// action, beside the evidence for the choice: the datasource's own type, the
// splits that statements state near the ex-date, and the actions other
// datasources report near it. It starts from the decision already recorded.
// A refusal stays in the dialog.
export function DecideCorpActDialog({
  corpActId,
  onClose,
}: {
  corpActId: string;
  onClose: () => void;
}) {
  const read = useCorpAct(corpActId);
  const decide = useDecideCorpAct();
  const [choice, setChoice] = useState<CorpActDecision | null>(null);
  const act = read.data?.corpAct;
  const selected = choice ?? act?.decision ?? CorpActDecision.UNSPECIFIED;
  const ready =
    selected !== CorpActDecision.UNSPECIFIED && !decide.isPending && !!act;
  return (
    <Dialog
      open
      onClose={onClose}
      title="Decide corporate action"
      testId="decide-dialog"
      footer={
        <>
          <Button
            variant="secondary"
            onClick={onClose}
            disabled={decide.isPending}
          >
            Cancel
          </Button>
          <Button
            data-testid="decide-save"
            disabled={!ready}
            onClick={() =>
              decide.mutate(
                { corpActId, decision: selected },
                { onSuccess: onClose },
              )
            }
          >
            Save
          </Button>
        </>
      }
    >
      {read.isError && (
        <Notice tone="error" onRetry={() => read.refetch()}>
          The corporate action could not be loaded.
        </Notice>
      )}
      {!read.isError && read.isPending && <Skeleton lines={4} />}
      {read.data && act && (
        <>
          <Action act={act} />
          <Evidence data={read.data} />
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-text-muted">Type</span>
            <Select
              data-testid="decide-type"
              value={selected}
              onChange={(e) => setChoice(Number(e.target.value))}
            >
              <option value={CorpActDecision.UNSPECIFIED} disabled>
                Choose a type
              </option>
              {enumValues(CorpActDecision).map((v) => (
                <option key={v} value={v}>
                  {enumLabel(CorpActDecision, v)}
                </option>
              ))}
            </Select>
          </label>
          <p className="text-sm text-text-muted">
            A split, a reverse split and a stock dividend restate the share
            count by the ratio. A spin-off price factor and a rights issue leave
            it. Other keeps the holding provisional across the ex-date.
          </p>
        </>
      )}
      {decide.isError && (
        <Notice tone="error" testId="decide-error">
          {refusal(decide.error, "The decision could not be recorded.")}
        </Notice>
      )}
    </Dialog>
  );
}

// Action is the header line that identifies the action being decided.
function Action({ act }: { act: CorpAct }) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      {act.ticker && <MarketTickerChip ticker={act.ticker} />}
      <span className="font-mono tabular-nums">{act.exDate}</span>
      <span className="font-mono tabular-nums">{formatRatio(act.ratio)}</span>
      <span className="text-text-muted">from {act.datasource}</span>
    </div>
  );
}

// Evidence lists what bears on the decision, one line per source, saying so
// where a source has nothing.
function Evidence({ data }: { data: GetCorpActResponse }) {
  const act = data.corpAct!;
  return (
    <dl data-testid="decide-evidence" className="flex flex-col gap-2 text-sm">
      <div>
        <dt className="text-text-muted">Stated type</dt>
        <dd>
          {act.type ? (
            <>
              {act.datasource} states{" "}
              <span className="font-mono">{act.type}</span>
            </>
          ) : (
            <>{act.datasource} did not state a type</>
          )}
        </dd>
      </div>
      <div>
        <dt className="text-text-muted">Statement splits within 7 days</dt>
        <dd>
          {data.stmtSplits.length === 0 ? (
            "None"
          ) : (
            <ul className="font-mono tabular-nums">
              {data.stmtSplits.map((s, i) => (
                <li key={i}>
                  {s.effectiveDate}
                  {s.ratio && `, ${formatRatio(s.ratio)}`}
                </li>
              ))}
            </ul>
          )}
        </dd>
      </div>
      <div>
        <dt className="text-text-muted">Other datasources within 7 days</dt>
        <dd>
          {data.otherCorpActs.length === 0 ? (
            "None"
          ) : (
            <ul>
              {data.otherCorpActs.map((o) => (
                <li key={o.id}>
                  {o.datasource}:{" "}
                  <span className="font-mono tabular-nums">
                    {o.exDate}, {formatRatio(o.ratio)}
                  </span>
                  {o.type && (
                    <>
                      , <span className="font-mono">{o.type}</span>
                    </>
                  )}
                </li>
              ))}
            </ul>
          )}
        </dd>
      </div>
    </dl>
  );
}
