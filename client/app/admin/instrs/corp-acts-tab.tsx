"use client";

import { useState } from "react";
import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { DecideCorpActDialog } from "@/app/components/decide-corp-act-dialog";
import { Notice } from "@/app/components/notice";
import { Skeleton } from "@/app/components/skeleton";
import { None, PaneTable, Td, Th } from "@/app/components/table";
import {
  type CorpAct,
  CorpActDecision,
} from "@/gen/admin/corpact/v1/corpact_pb";
import { useInstrCorpActs } from "@/hooks/use-corp-acts";
import { enumLabel } from "@/lib/enum";
import { formatRatio } from "@/lib/format";

// unknown reports whether the action's effect on the quantity held is
// unknown: neither a decision nor a system judgment settles it, or it is
// decided other. The corporate action alert fires on the same condition.
export function unknown(a: CorpAct): boolean {
  return (
    (a.decision === CorpActDecision.UNSPECIFIED && !a.restatesShares) ||
    a.decision === CorpActDecision.OTHER
  );
}

// CorpActsTab lists the stored corporate actions of an instrument, latest
// first, with the effective restates-shares and the decision of each, and
// opens the decide dialog on one.
export function CorpActsTab({ instrId }: { instrId: string }) {
  const { data, isPending, isError, refetch } = useInstrCorpActs(instrId);
  const [deciding, setDeciding] = useState<string | null>(null);
  const acts = data?.corpActs ?? [];
  return (
    <div data-testid="corp-acts-tab" className="flex flex-col gap-6">
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The corporate actions could not be loaded.
        </Notice>
      )}
      {!isError && isPending && <Skeleton lines={2} />}
      {data && acts.length === 0 && <None />}
      {acts.length > 0 && (
        <PaneTable testId="corp-acts-table">
          <thead className="border-b border-border">
            <tr>
              <Th dense>Ex-date</Th>
              <Th dense>Ticker</Th>
              <Th dense>Ratio</Th>
              <Th dense>Datasource</Th>
              <Th dense>Type</Th>
              <Th dense>Restates shares</Th>
              <Th dense>Decision</Th>
              <Th dense>
                <span className="sr-only">Decide</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {acts.map((a) => (
              <tr
                key={a.id}
                data-testid={`corp-act-row-${a.id}`}
                data-decision={CorpActDecision[a.decision]}
              >
                <Td dense className="font-mono tabular-nums">
                  {a.exDate}
                </Td>
                <Td dense>
                  <span className="font-mono">{a.ticker?.value}</span>
                  <span className="text-text-muted">
                    {" "}
                    in {a.ticker?.market}
                  </span>
                </Td>
                <Td dense className="font-mono tabular-nums">
                  {formatRatio(a.ratio)}
                </Td>
                <Td dense>{a.datasource}</Td>
                <Td dense className="font-mono">
                  {a.type ?? <span className="text-text-muted">none</span>}
                </Td>
                <Td dense>
                  {a.applied ? (
                    a.restatesShares ? (
                      "Yes"
                    ) : (
                      "No"
                    )
                  ) : (
                    <Chip tone="muted">Not applied</Chip>
                  )}
                </Td>
                <Td dense>
                  <Chip tone={unknown(a) ? "accent" : "muted"}>
                    {a.decision === CorpActDecision.UNSPECIFIED
                      ? "Undecided"
                      : enumLabel(CorpActDecision, a.decision)}
                  </Chip>
                </Td>
                <Td dense className="text-right">
                  <Button
                    variant={unknown(a) ? "primary" : "secondary"}
                    data-testid={`corp-act-decide-${a.id}`}
                    onClick={() => setDeciding(a.id)}
                  >
                    Decide
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </PaneTable>
      )}
      {deciding && (
        <DecideCorpActDialog
          corpActId={deciding}
          onClose={() => setDeciding(null)}
        />
      )}
    </div>
  );
}
