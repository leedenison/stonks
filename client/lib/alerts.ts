import {
  type Alert,
  AlertKind,
  type GetAlertDigestResponse,
} from "@/gen/admin/alert/v1/alert_pb";
import { FetchKind } from "@/gen/admin/block/v1/block_pb";
import { FindingKind } from "@/gen/admin/run/v1/run_pb";
import type { Identifier, MarketTicker } from "@/gen/type/v1/type_pb";
import { findingText, runTitle } from "./admin";
import { dayRange } from "./day-range";
import { enumLabel, enumParam, fromParam } from "./enum";
import { formatInstant } from "./format";

// AlertAction is what a row's button does.
export type AlertAction =
  | { case: "open-run"; runId: string }
  | { case: "clear-block"; id: string }
  | { case: "attest"; ticker: MarketTicker; from: string; to?: string }
  | { case: "decide"; corpActId: string }
  | { case: "clear-finding"; id: string };

// AlertItem is what the Item cell shows.
export type AlertItem = {
  label?: string;
  // The page the label opens.
  href?: string;
  tag?: string;
  ticker?: MarketTicker;
  sent?: Identifier;
};

export type AlertRow = {
  // Unique among the alerts of a kind: a ticker can have several gaps.
  key: string;
  kind: AlertKind;
  item: AlertItem;
  detail: string;
  sub?: string;
  txns?: number;
  keys?: number;
  // The date the alert dates from.
  since: string;
  action: AlertAction;
};

// kindOrder is the order of the digest headings and the tiles.
export const kindOrder: AlertKind[] = [
  AlertKind.FAILED_RUN,
  AlertKind.BLOCK,
  AlertKind.TICKER_NAMES_NOTHING,
  AlertKind.TICKER_VALIDITY_GAP,
  AlertKind.CORP_ACT,
  AlertKind.FINDING,
];

type Words = { tile: string; group: string; why: string; action: string };

const words: Record<number, Words> = {
  [AlertKind.FAILED_RUN]: {
    tile: "failed runs",
    group: "Failed runs",
    why: "nothing downstream of the run happened",
    action: "Open run",
  },
  [AlertKind.BLOCK]: {
    tile: "blocks",
    group: "Blocks",
    why: "a datasource is not asked again until the block is cleared",
    action: "Clear",
  },
  [AlertKind.TICKER_NAMES_NOTHING]: {
    tile: "tickers naming nothing",
    group: "Tickers that name nothing",
    why: "the ticker names no instrument on any day, so it has no instrument page",
    action: "Attest",
  },
  [AlertKind.TICKER_VALIDITY_GAP]: {
    tile: "validity gaps",
    group: "Ticker validity gaps",
    why: "part of the transaction period lies outside every confirmed interval",
    action: "Attest",
  },
  [AlertKind.CORP_ACT]: {
    tile: "undecided corporate actions",
    group: "Corporate actions",
    why: "the action stays out of the adjusted holdings until an administrator decides its type",
    action: "Decide",
  },
  [AlertKind.FINDING]: {
    tile: "findings to check",
    group: "Findings",
    why: "the system chose between sources or found a mismatch, and an administrator checks it",
    action: "Clear",
  },
};

export function kindWords(kind: AlertKind): Words {
  return words[kind];
}

// kindSlug is the kind's name in the address and in test ids.
export function kindSlug(kind: AlertKind): string {
  return enumParam(AlertKind, kind);
}

// AlertParams is the alerts page's address state. An undefined kind is the
// digest.
export type AlertParams = { kind: AlertKind | undefined; before: string };

export function readAlertParams(params: URLSearchParams): AlertParams {
  return {
    kind: fromParam(AlertKind, params.get("kind") ?? ""),
    before: params.get("before") ?? "",
  };
}

// openCount is the number of open alerts of every kind.
export function openCount(digest: GetAlertDigestResponse | undefined): number {
  return digest?.groups.reduce((n, g) => n + g.count, 0) ?? 0;
}

const day = (s: string | undefined) => s ?? "";

// alertRow maps an alert to the row the table and the actions render from. It
// is the one place that reads the oneof.
export function alertRow(a: Alert): AlertRow {
  const s = a.subject;
  switch (s.case) {
    case "run": {
      const id = s.value.run?.id ?? "";
      return {
        key: id,
        kind: a.kind,
        item: { label: `run ${id.slice(0, 8)}`, href: `/admin/runs/${id}` },
        detail: runTitle(s.value),
        sub: s.value.run?.error,
        since: formatInstant(s.value.run?.createdAt),
        action: { case: "open-run", runId: id },
      };
    }
    case "block": {
      const b = s.value;
      return {
        key: b.id,
        kind: a.kind,
        item: {
          label: b.datasource,
          tag: enumLabel(FetchKind, b.kind),
          sent: b.sent,
        },
        detail: b.reason,
        sub: `run ${b.runId}`,
        since: formatInstant(b.createdAt),
        action: { case: "clear-block", id: b.id },
      };
    }
    case "namesNothing": {
      const n = s.value;
      const t = n.ticker;
      return {
        key: `${t?.market}-${t?.value}`,
        kind: a.kind,
        item: { ticker: t },
        detail: "Names nothing on any day",
        txns: n.txns,
        keys: n.keys,
        since: n.firstTxnDay,
        action: {
          case: "attest",
          ticker: t!,
          from: n.firstTxnDay,
        },
      };
    }
    case "validityGap": {
      const g = s.value;
      const t = g.ticker;
      const r = dayRange(g.gap?.period);
      return {
        key: `${t?.market}-${t?.value}-${r.from}`,
        kind: a.kind,
        item: { ticker: t, label: g.instr?.name?.value },
        detail: `No confirmed interval from ${r.from}${r.to ? ` to ${r.to}` : ""}`,
        txns: g.gap?.txns,
        keys: g.gap?.keys,
        since: r.from,
        action: {
          case: "attest",
          ticker: t!,
          from: r.from,
          to: r.to || undefined,
        },
      };
    }
    case "finding": {
      const f = s.value;
      const corp = a.kind === AlertKind.CORP_ACT;
      return {
        key: f.id,
        kind: a.kind,
        item: { label: enumLabel(FindingKind, f.kind) },
        detail: findingText(f),
        sub: `run ${f.runId}`,
        since: formatInstant(f.createdAt),
        action: corp
          ? { case: "decide", corpActId: day(f.corpActId) }
          : { case: "clear-finding", id: f.id },
      };
    }
    default:
      throw new Error("alert without a subject");
  }
}
