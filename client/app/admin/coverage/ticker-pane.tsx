"use client";

import { create } from "@bufbuild/protobuf";
import { useState } from "react";
import { AttestDialog } from "@/app/components/attest-dialog";
import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { Dialog } from "@/app/components/dialog";
import { IdentifierChip } from "@/app/components/identifier-chip";
import { Notice } from "@/app/components/notice";
import { Section } from "@/app/components/section";
import { Skeleton } from "@/app/components/skeleton";
import { Td, Th } from "@/app/components/table";
import {
  IdentifierEventAction,
  type Ticker,
} from "@/gen/admin/coverage/v1/coverage_pb";
import type { AssertionItem } from "@/gen/admin/run/v1/run_pb";
import { IdentifierSchema, IdentifierType } from "@/gen/type/v1/type_pb";
import {
  useCoverageTicker,
  useWithdrawAttestation,
} from "@/hooks/use-coverage";
import { instrumentName, intervalAt } from "@/lib/coverage";
import { enumLabel } from "@/lib/enum";
import { refusal } from "@/lib/refusal";
import { Timeline } from "./timeline";

// TickerPane shows everything the coverage page knows about one ticker, and
// offers an attestation from each gap and the withdrawal of each attested
// assertion.
export function TickerPane({
  domain,
  value,
}: {
  domain: string;
  value: string;
}) {
  const { data, isPending, isError, refetch } = useCoverageTicker(
    domain,
    value,
  );
  const [attesting, setAttesting] = useState<string | null>(null);
  const [withdrawing, setWithdrawing] = useState<AssertionItem | null>(null);
  const ticker = create(IdentifierSchema, {
    type: IdentifierType.MIC_TICKER,
    domain,
    value,
  });
  return (
    <div
      data-testid="ticker-pane"
      className="flex min-w-0 flex-1 flex-col gap-6 rounded-md border border-border bg-surface px-5 py-4 shadow-xs"
    >
      <div className="flex flex-wrap items-baseline gap-3">
        <h2 className="font-mono text-xl font-semibold tracking-tight">
          {value}
        </h2>
        <Chip className="font-mono">{domain}</Chip>
      </div>
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The ticker could not be loaded.
        </Notice>
      )}
      {!isError && isPending && <Skeleton lines={4} />}
      {data?.ticker && (
        <Facts
          ticker={data.ticker}
          onAttest={setAttesting}
          onWithdraw={setWithdrawing}
        />
      )}
      {attesting !== null && (
        <AttestDialog
          ticker={ticker}
          from={attesting}
          onClose={() => setAttesting(null)}
          onDone={() => setAttesting(null)}
        />
      )}
      {withdrawing && (
        <WithdrawDialog
          assertion={withdrawing}
          onClose={() => setWithdrawing(null)}
        />
      )}
    </div>
  );
}

// WithdrawDialog confirms the withdrawal of an attested assertion. A refusal
// stays in the dialog.
function WithdrawDialog({
  assertion,
  onClose,
}: {
  assertion: AssertionItem;
  onClose: () => void;
}) {
  const withdraw = useWithdrawAttestation();
  return (
    <Dialog
      open
      onClose={onClose}
      title="Withdraw attestation"
      testId="withdraw-dialog"
      footer={
        <>
          <Button
            variant="secondary"
            onClick={onClose}
            disabled={withdraw.isPending}
          >
            Cancel
          </Button>
          <Button
            data-testid="withdraw-confirm"
            disabled={withdraw.isPending}
            onClick={() =>
              withdraw.mutate(assertion.id, { onSuccess: onClose })
            }
          >
            Withdraw
          </Button>
        </>
      }
    >
      <p className="text-sm">
        Deletes the assertion of {assertion.assertedOn} and replays the
        unresolved keys that state the ticker. A dated key picks up the change
        the next time it is read.
      </p>
      {withdraw.isError && (
        <Notice tone="error" testId="withdraw-error">
          {refusal(withdraw.error, "The attestation could not be withdrawn.")}
        </Notice>
      )}
    </Dialog>
  );
}

// Instrument shows the identifier that names an instrument, or the id where
// the ticker gives no name for it.
function Instrument({ ticker, id }: { ticker: Ticker; id?: string }) {
  if (id === undefined) {
    return <span className="text-text-muted">nothing</span>;
  }
  const name = instrumentName(ticker, id);
  return name ? (
    <IdentifierChip id={name} />
  ) : (
    <span className="font-mono text-xs text-text-muted">{id}</span>
  );
}

function Facts({
  ticker,
  onAttest,
  onWithdraw,
}: {
  ticker: Ticker;
  onAttest: (from: string) => void;
  onWithdraw: (a: AssertionItem) => void;
}) {
  return (
    <>
      <Timeline ticker={ticker} />
      <Section title="Gaps">
        {ticker.gaps.length === 0 ? (
          <None />
        ) : (
          <Table testId="ticker-gaps">
            <thead className="border-b border-border">
              <tr>
                <Th dense>From</Th>
                <Th dense>Before</Th>
                <Th dense>Instrument</Th>
                <Th dense numeric>
                  Keys
                </Th>
                <Th dense numeric>
                  Transactions
                </Th>
                <Th dense> </Th>
              </tr>
            </thead>
            <tbody>
              {ticker.gaps.map((g) => (
                <tr
                  key={g.unconfirmedFrom}
                  data-testid={`pane-gap-${g.unconfirmedFrom}`}
                >
                  <Td dense className="font-mono tabular-nums">
                    {g.unconfirmedFrom}
                  </Td>
                  <Td dense className="font-mono tabular-nums">
                    {g.unconfirmedBefore}
                  </Td>
                  <Td dense>
                    <Instrument
                      ticker={ticker}
                      id={
                        intervalAt(ticker.intervals, g.unconfirmedFrom)
                          ?.instrumentId
                      }
                    />
                  </Td>
                  <Td dense numeric>
                    {g.keys}
                  </Td>
                  <Td dense numeric>
                    {g.transactions}
                  </Td>
                  <Td dense>
                    <Button
                      variant="secondary"
                      data-testid={`gap-attest-${g.unconfirmedFrom}`}
                      onClick={() => onAttest(g.unconfirmedFrom)}
                    >
                      Attest
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Section>
      <Section title="Assertions">
        {ticker.assertions.length === 0 ? (
          <None />
        ) : (
          <Table testId="ticker-assertions">
            <thead className="border-b border-border">
              <tr>
                <Th dense>Day</Th>
                <Th dense>Instrument</Th>
                <Th dense>Source</Th>
                <Th dense> </Th>
              </tr>
            </thead>
            <tbody>
              {ticker.assertions.map((a) => (
                <tr
                  key={a.id}
                  data-testid={`assertion-row-${a.id}`}
                  data-attested={
                    a.source.case === "attestedBy" ? "true" : undefined
                  }
                >
                  <Td dense className="font-mono tabular-nums">
                    {a.assertedOn}
                  </Td>
                  <Td dense>
                    <Instrument ticker={ticker} id={a.instrumentId} />
                  </Td>
                  <Td dense>
                    <Chip
                      tone={a.source.case === "attestedBy" ? "accent" : "muted"}
                    >
                      {a.source.value}
                    </Chip>
                  </Td>
                  <Td dense>
                    {a.source.case === "attestedBy" && (
                      <Button
                        variant="secondary"
                        data-testid={`assertion-withdraw-${a.id}`}
                        onClick={() => onWithdraw(a)}
                      >
                        Withdraw
                      </Button>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Section>
      {ticker.events.length > 0 && (
        <Section title="Events">
          <Table testId="ticker-events">
            <thead className="border-b border-border">
              <tr>
                <Th dense>Day</Th>
                <Th dense>Action</Th>
                <Th dense>Ticker</Th>
                <Th dense>To</Th>
              </tr>
            </thead>
            <tbody>
              {ticker.events.map((e, i) => (
                <tr key={i}>
                  <Td dense className="font-mono tabular-nums">
                    {e.effectiveOn}
                  </Td>
                  <Td dense>
                    <Chip>{enumLabel(IdentifierEventAction, e.action)}</Chip>
                  </Td>
                  <Td dense className="font-mono">
                    {e.value}
                  </Td>
                  <Td dense className="font-mono">
                    {e.toValue}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Section>
      )}
      <Section title="Coverage">
        {ticker.coverage.length === 0 ? (
          <None />
        ) : (
          <Table testId="ticker-coverage">
            <thead className="border-b border-border">
              <tr>
                <Th dense>Datasource</Th>
                <Th dense>Covers</Th>
                <Th dense>From</Th>
                <Th dense>Before</Th>
              </tr>
            </thead>
            <tbody>
              {ticker.coverage.map((c, i) => (
                <tr key={i}>
                  <Td dense>
                    <Chip>{c.datasource}</Chip>
                  </Td>
                  <Td dense className="font-mono">
                    {c.covers?.value || `every ticker of ${c.covers?.domain}`}
                  </Td>
                  <Td dense className="font-mono tabular-nums">
                    {c.coveredFrom}
                  </Td>
                  <Td dense className="font-mono tabular-nums">
                    {c.coveredBefore}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Section>
    </>
  );
}

function None() {
  return <p className="text-sm text-text-muted">None.</p>;
}

// Table frames a pane table.
function Table({
  testId,
  children,
}: {
  testId: string;
  children: React.ReactNode;
}) {
  return (
    <table
      data-testid={testId}
      className="w-full border-collapse border-b border-border text-sm"
    >
      {children}
    </table>
  );
}
