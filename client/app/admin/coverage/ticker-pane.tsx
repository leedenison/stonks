"use client";

import { Chip } from "@/app/components/chip";
import { IdentifierChip } from "@/app/components/identifier-chip";
import { Notice } from "@/app/components/notice";
import { Section } from "@/app/components/section";
import { Skeleton } from "@/app/components/skeleton";
import { Td, Th } from "@/app/components/table";
import {
  IdentifierEventAction,
  type Ticker,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { useCoverageTicker } from "@/hooks/use-coverage";
import { instrumentName, intervalAt } from "@/lib/coverage";
import { enumLabel } from "@/lib/enum";
import { Timeline } from "./timeline";

// TickerPane shows everything the coverage page knows about one ticker.
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
      {data?.ticker && <Facts ticker={data.ticker} />}
    </div>
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

function Facts({ ticker }: { ticker: Ticker }) {
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
