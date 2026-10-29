import { create } from "@bufbuild/protobuf";
import Link from "next/link";
import type { ReactNode } from "react";
import { Chip } from "@/app/components/chip";
import { identifierLabel } from "@/app/components/identifier-chip";
import { Section } from "@/app/components/section";
import { None, PaneTable, Td, Th } from "@/app/components/table";
import type { Instr, Listing_Ticker } from "@/gen/admin/instr/v1/instr_pb";
import {
  type Identifier,
  IdentifierSchema,
  IdentifierType,
  type TickerRelation,
} from "@/gen/type/v1/type_pb";
import {
  type InstrParams,
  instrQuery,
  relationSuffix,
  tickerParam,
} from "@/lib/admin";
import { assetClassLabel } from "@/lib/asset-class";
import { groupIdentifiers, identifierKey } from "@/lib/identifiers";
import { TickersTab } from "./tickers-tab";

type Marks = Parameters<typeof groupIdentifiers>[1];

const tabs = [
  { key: "", label: "Overview", testId: "overview" },
  { key: "tickers", label: "Tickers", testId: "tickers" },
] as const;

// InstrPane shows one instrument under two tabs held in the address:
// the overview, with its identifiers and listings in sections, and its
// tickers, with the ticker validity of each.
export function InstrPane({
  instr,
  params,
}: {
  instr: Instr;
  params: InstrParams;
}) {
  const tab = params.tab === "tickers" ? "tickers" : "";
  return (
    <div
      data-testid="instr-pane"
      className="flex min-w-0 flex-1 flex-col gap-6 rounded-md border border-border bg-surface px-5 py-4 shadow-xs"
    >
      <div className="flex flex-wrap items-baseline gap-3">
        <h2 className="text-xl font-semibold tracking-tight">
          {instr.name?.value || instr.id}
        </h2>
        <Chip>{assetClassLabel(instr.assetClass)}</Chip>
        <span className="font-mono text-xs text-text-muted">{instr.id}</span>
      </div>
      <nav role="tablist" className="flex gap-6 border-b border-border text-sm">
        {tabs.map((t) => {
          const active = t.key === tab;
          return (
            <Link
              key={t.key}
              role="tab"
              aria-selected={active}
              data-testid={`instr-tab-${t.testId}`}
              href={instrQuery({ ...params, tab: t.key, ticker: "" })}
              className={`-mb-px border-b-2 pb-2 transition-colors ${
                active
                  ? "border-accent font-semibold text-primary-dark dark:text-primary"
                  : "border-transparent text-text-muted hover:text-primary"
              }`}
            >
              {t.label}
            </Link>
          );
        })}
      </nav>
      {tab === "tickers" ? (
        <TickersTab instrId={instr.id} params={params} />
      ) : (
        <Overview instr={instr} params={params} />
      )}
    </div>
  );
}

// Overview shows the instrument's identifiers and listings in sections. The
// identifier naming the instrument and each listing's primary venue are
// marked in the accent tone. A ticker's chip opens the tickers tab on that
// ticker.
function Overview({ instr, params }: { instr: Instr; params: InstrParams }) {
  const marks: Marks = {
    via: new Set(instr.via.map(identifierKey)),
    name: new Set(instr.name ? [identifierKey(instr.name)] : []),
  };
  return (
    <>
      <Section title="Identifiers">
        {instr.identifiers.length === 0 ? (
          <None />
        ) : (
          <IdentifierTable valueHead="Value" testId="instr-identifiers">
            <tbody>
              <Rows ids={instr.identifiers} marks={marks} />
            </tbody>
          </IdentifierTable>
        )}
      </Section>
      <Section title="Listings">
        {instr.listings.length === 0 ? (
          <None />
        ) : (
          <IdentifierTable valueHead="Symbol">
            {instr.listings.map((l) => (
              <tbody key={l.id} data-testid={`listing-${l.id}`}>
                <tr>
                  <td colSpan={3} className="bg-surface-tint px-3 py-2">
                    <span className="flex items-baseline gap-3">
                      <span className="font-mono font-semibold tabular-nums">
                        {l.currency}
                      </span>
                      {l.primaryMic && (
                        <span className="text-text-muted">
                          Primary{" "}
                          <span className="font-mono">{l.primaryMic}</span>
                        </span>
                      )}
                    </span>
                  </td>
                </tr>
                <TickerRows
                  tickers={l.tickers}
                  named={marks.name}
                  params={params}
                  primary={l.primaryMic}
                />
                {(l.identifiers.length > 0 || l.tickers.length === 0) && (
                  <Rows ids={l.identifiers} marks={marks} />
                )}
              </tbody>
            ))}
          </IdentifierTable>
        )}
      </Section>
    </>
  );
}

// IdentifierTable is the header and frame for identifier rows, which the
// caller supplies as tbody children.
function IdentifierTable({
  valueHead,
  testId,
  children,
}: {
  valueHead: string;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <PaneTable testId={testId} fixed>
      <colgroup>
        <col className="w-[16%]" />
        <col className="w-[16%]" />
        <col />
      </colgroup>
      <thead className="border-b border-border">
        <tr>
          <Th dense>Type</Th>
          <Th dense>{valueHead}</Th>
          <Th dense>Domain</Th>
        </tr>
      </thead>
      {children}
    </PaneTable>
  );
}

// A TickerGroup is one ticker of a listing with the venues it trades at.
type TickerGroup = {
  market: string;
  value: string;
  relation: TickerRelation;
  venues: string[];
};

// groupTickers gathers a listing's tickers into one group per market and
// value, in the order the API lists them.
function groupTickers(ts: Listing_Ticker[]): TickerGroup[] {
  const groups = new Map<string, TickerGroup>();
  for (const t of ts) {
    const market = t.ticker?.market ?? "";
    const value = t.ticker?.value ?? "";
    const key = tickerParam(market, value);
    let g = groups.get(key);
    if (!g) {
      g = { market, value, relation: t.relation, venues: [] };
      groups.set(key, g);
    }
    if (t.mic) {
      g.venues.push(t.mic);
    }
  }
  return [...groups.values()];
}

// TickerRows renders a listing's tickers, one row for each, with the venues
// of each in the domain column. It returns bare rows, so the caller places
// it inside a tbody.
function TickerRows({
  tickers,
  named,
  params,
  primary,
}: {
  tickers: Listing_Ticker[];
  // The identifierKey of the identifier naming the instrument, if any.
  named?: Set<string>;
  params: InstrParams;
  // The venue to mark as the primary; empty for none.
  primary: string;
}) {
  const groups = groupTickers(tickers);
  return groups.map((g, i) => {
    const name = [...g.venues, g.market].some((at) =>
      named?.has(
        identifierKey(
          create(IdentifierSchema, {
            type: IdentifierType.MIC_TICKER,
            domain: at,
            value: g.value,
          }),
        ),
      ),
    );
    return (
      <tr
        key={tickerParam(g.market, g.value)}
        data-identifier-type={IdentifierType[IdentifierType.MIC_TICKER]}
        data-name={name ? "true" : undefined}
      >
        {i === 0 && (
          <Td
            dense
            rowSpan={groups.length}
            className="align-top text-text-muted"
          >
            {identifierLabel(IdentifierType.MIC_TICKER)}
          </Td>
        )}
        <Td dense>
          <Link
            href={instrQuery({
              ...params,
              tab: "tickers",
              ticker: tickerParam(g.market, g.value),
            })}
            data-testid={`ticker-validity-${g.market}-${g.value}`}
          >
            <Chip tone={name ? "accent" : "primary"} className="font-mono">
              {g.value}
            </Chip>
          </Link>
          <span className="text-text-muted"> in {g.market}</span>
          {relationSuffix[g.relation]}
        </Td>
        <Td dense>
          <span className="flex flex-wrap gap-1">
            {g.venues.map((v) => (
              <Chip
                key={v}
                tone={v === primary ? "accent" : "muted"}
                data-primary={v === primary ? "true" : undefined}
                className="font-mono"
              >
                {v}
              </Chip>
            ))}
          </span>
        </Td>
      </tr>
    );
  });
}

// Rows renders identifiers grouped by type. It returns bare rows, so the
// caller places it inside a tbody.
function Rows({ ids, marks }: { ids: Identifier[]; marks: Marks }) {
  if (ids.length === 0) {
    return (
      <tr>
        <Td dense colSpan={3} className="text-text-muted">
          None.
        </Td>
      </tr>
    );
  }
  const groups = groupIdentifiers(ids, marks);
  const span = new Map<IdentifierType, number>();
  for (const g of groups) {
    span.set(g.type, (span.get(g.type) ?? 0) + 1);
  }
  const rows = groups.map((g, i) => ({
    ...g,
    first: i === 0 || groups[i - 1].type !== g.type,
  }));
  return rows.map((r) => (
    <tr
      key={`${r.type}-${r.value}`}
      data-identifier-type={IdentifierType[r.type]}
      data-via={r.via ? "true" : undefined}
      data-name={r.name ? "true" : undefined}
    >
      {r.first && (
        <Td
          dense
          rowSpan={span.get(r.type)}
          className="align-top text-text-muted"
        >
          {identifierLabel(r.type)}
        </Td>
      )}
      <Td dense>
        <Chip tone={r.name ? "accent" : "primary"} className="font-mono">
          {r.value}
        </Chip>
      </Td>
      <Td dense>
        <span className="flex flex-wrap gap-1">
          {r.domains.map((d) => (
            <Chip key={d} tone="muted" className="font-mono">
              {d}
            </Chip>
          ))}
        </span>
      </Td>
    </tr>
  ));
}
