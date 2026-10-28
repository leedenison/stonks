"use client";

import { create } from "@bufbuild/protobuf";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { AttestIdentifierDialog } from "@/app/components/attest-identifier-dialog";
import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { Dialog } from "@/app/components/dialog";
import { IdentifierChip } from "@/app/components/identifier-chip";
import { Select } from "@/app/components/input";
import { Notice } from "@/app/components/notice";
import { Section } from "@/app/components/section";
import { Skeleton } from "@/app/components/skeleton";
import { None, PaneTable, Td, Th } from "@/app/components/table";
import type { InstrTicker, Ticker } from "@/gen/admin/coverage/v1/coverage_pb";
import type { AssertionItem } from "@/gen/admin/run/v1/run_pb";
import { MarketTickerSchema } from "@/gen/type/v1/type_pb";
import {
  useCoverageTicker,
  useInstrTickers,
  useWithdrawIdentifierAttestation,
} from "@/hooks/use-coverage";
import {
  type InstrParams,
  instrQuery,
  readTickerParam,
  relationSuffix,
  tickerParam,
} from "@/lib/admin";
import { bounds, cutDays, instrName, intervalAt } from "@/lib/coverage";
import { today } from "@/lib/marshal/date";
import { refusal } from "@/lib/refusal";
import { Timeline } from "./timeline";

// TickersTab lists the tickers that name an instrument, now or in an assertion,
// one per market, and shows the ticker validity of the one chosen. The choice
// is held in the address, at the market or at a venue of it, and the first
// listed ticker stands in while it is unset.
export function TickersTab({
  instrId,
  params,
}: {
  instrId: string;
  params: InstrParams;
}) {
  const router = useRouter();
  const { data, isPending, isError, refetch } = useInstrTickers(instrId);
  const listed = data?.tickers ?? [];
  const wanted = readTickerParam(params.ticker);
  const chosen =
    listed.find(
      (t) =>
        t.ticker?.value === wanted?.value &&
        (t.ticker?.market === wanted?.at ||
          t.venues.includes(wanted?.at ?? "")),
    ) ?? listed[0];
  const param = (t: InstrTicker) =>
    tickerParam(t.ticker?.market ?? "", t.ticker?.value ?? "");
  return (
    <div data-testid="tickers-tab" className="flex flex-col gap-6">
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The tickers could not be loaded.
        </Notice>
      )}
      {!isError && isPending && <Skeleton lines={2} />}
      {data && listed.length === 0 && (
        <p data-testid="no-tickers" className="text-sm text-text-muted">
          No ticker names this instrument.
        </p>
      )}
      {chosen && (
        <>
          <label className="flex items-center gap-2 text-sm">
            <span className="text-text-muted">Ticker</span>
            <Select
              data-testid="ticker-select"
              value={param(chosen)}
              onChange={(e) =>
                router.replace(
                  instrQuery({ ...params, ticker: e.target.value }),
                )
              }
            >
              {listed.map((t) => (
                <option key={param(t)} value={param(t)}>
                  {t.ticker?.value} in {t.ticker?.market}
                  {relationSuffix[t.relation]}
                </option>
              ))}
            </Select>
          </label>
          <TickerSection
            market={chosen.ticker?.market ?? ""}
            value={chosen.ticker?.value ?? ""}
          />
        </>
      )}
    </div>
  );
}

// TickerSection shows everything known about one ticker and the actions that
// settle it.
function TickerSection({ market, value }: { market: string; value: string }) {
  const { data, isPending, isError, refetch } = useCoverageTicker(
    market,
    value,
  );
  const [attesting, setAttesting] = useState<string | null>(null);
  const [withdrawing, setWithdrawing] = useState<AssertionItem | null>(null);
  const ticker = create(MarketTickerSchema, { market, value });
  return (
    <Section
      title={
        <span className="flex flex-wrap items-baseline gap-3">
          Ticker validity
          {data?.ticker && (
            <Button
              variant="text"
              data-testid="ticker-attest-today"
              onClick={() => setAttesting(today())}
            >
              Attest {value} today
            </Button>
          )}
        </span>
      }
    >
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The ticker could not be loaded.
        </Notice>
      )}
      {!isError && isPending && <Skeleton lines={4} />}
      {data?.ticker &&
        (bounds(data.ticker) ? (
          <Facts
            ticker={data.ticker}
            onAttest={setAttesting}
            onWithdraw={setWithdrawing}
          />
        ) : (
          <p data-testid="no-facts" className="text-sm text-text-muted">
            No facts for this ticker.
          </p>
        ))}
      {attesting !== null && (
        <AttestIdentifierDialog
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
    </Section>
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
  const withdraw = useWithdrawIdentifierAttestation();
  return (
    <Dialog
      open
      onClose={onClose}
      title="Withdraw identifier attestation"
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
        Deletes the assertion of the days from {assertion.assertedFrom} up to{" "}
        {assertion.assertedBefore} and replays the unresolved keys that state
        the ticker. A dated key picks up the change the next time it is read.
      </p>
      {withdraw.isError && (
        <Notice tone="error" testId="withdraw-error">
          {refusal(
            withdraw.error,
            "The identifier attestation could not be withdrawn.",
          )}
        </Notice>
      )}
    </Dialog>
  );
}

// Instr shows the identifier that names an instrument, or the id when
// the ticker does not name it.
function Instr({ ticker, id }: { ticker: Ticker; id?: string }) {
  if (id === undefined) {
    return <span className="text-text-muted">nothing</span>;
  }
  const name = instrName(ticker, id);
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
    <div className="flex flex-col gap-6">
      <Timeline ticker={ticker} />
      <Section title="Gaps">
        {ticker.gaps.length === 0 ? (
          <None />
        ) : (
          <PaneTable testId="ticker-gaps">
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
                    <Instr
                      ticker={ticker}
                      id={
                        intervalAt(ticker.intervals, g.unconfirmedFrom)?.instrId
                      }
                    />
                  </Td>
                  <Td dense numeric>
                    {g.keys}
                  </Td>
                  <Td dense numeric>
                    {g.txns}
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
          </PaneTable>
        )}
      </Section>
      <Section title="Assertions">
        {ticker.assertions.length === 0 ? (
          <None />
        ) : (
          <PaneTable testId="ticker-assertions">
            <thead className="border-b border-border">
              <tr>
                <Th dense>From</Th>
                <Th dense>Before</Th>
                <Th dense>Instrument</Th>
                <Th dense>Edges</Th>
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
                    {a.assertedFrom}
                  </Td>
                  <Td dense className="font-mono tabular-nums">
                    {a.assertedBefore}
                  </Td>
                  <Td dense>
                    <Instr ticker={ticker} id={a.instrId} />
                  </Td>
                  <Td dense>
                    {cutDays(a).map((cut) => (
                      <Chip key={cut}>{cut}</Chip>
                    ))}
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
          </PaneTable>
        )}
      </Section>
      <Section title="Assertion coverage">
        {ticker.assertionCoverage.length === 0 ? (
          <None />
        ) : (
          <PaneTable testId="ticker-assertion-coverage">
            <thead className="border-b border-border">
              <tr>
                <Th dense>Datasource</Th>
                <Th dense>Holds</Th>
                <Th dense>From</Th>
                <Th dense>Before</Th>
              </tr>
            </thead>
            <tbody>
              {ticker.assertionCoverage.map((p, i) => (
                <tr key={i}>
                  <Td dense>
                    <Chip>{p.datasource}</Chip>
                  </Td>
                  <Td dense className="font-mono">
                    {p.value ?? `every ticker of ${p.market}`}
                  </Td>
                  <Td dense className="font-mono tabular-nums">
                    {p.coveredFrom}
                  </Td>
                  <Td dense className="font-mono tabular-nums">
                    {p.coveredBefore}
                  </Td>
                </tr>
              ))}
            </tbody>
          </PaneTable>
        )}
      </Section>
    </div>
  );
}
