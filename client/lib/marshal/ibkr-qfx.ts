// The IBKR QFX investment statement: OFX 1.02 SGML, read by ofx-js into a
// tree of tags. The statement is ASCII though its header declares CHARSET
// 1252, so reading the file as UTF-8 is safe for the statements seen.
//
// A trade, an income and a bank transaction state a currency, and the
// account's base currency CURDEF stands in where one is absent. A transfer
// states none, and its key is left without one rather than given the base
// currency, which would contradict the listing the description names.
//
// Every date is stated in the exchange's local zone with its offset, and the
// date part is taken as stated: an evening posting is not moved to the next
// UTC day.
//
// A row is stated as traded, and a split arrives as a transfer of the units
// it added, so every row is as at its order date.
//
// The security list prints an option's ticker in OCC form for a contract OCC
// lists and in IBKR's own form otherwise. A ticker in OCC form is carried as
// an OCC identifier only when the terms the same record states name that
// symbol; a record whose printed symbol and terms disagree fails the file.

import { parseSync } from "ofx-js";
import {
  AssetClass,
  Broker,
  type Identifier,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import type { Row, StatedSplit, Upload } from "@/gen/upload/v1/upload_pb";
import {
  cashKey,
  ident,
  leg,
  securityKey,
  split,
  trade,
  upload,
} from "./build";
import { iso } from "./date";
import { MarshalError } from "./error";
import { buildOcc, isOcc } from "./occ";

type Node = Record<string, unknown>;

function isNode(v: unknown): v is Node {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function node(parent: Node, name: string): Node {
  const v = parent[name];
  if (!isNode(v)) throw new MarshalError(`missing ${name}`, undefined, name);
  return v;
}

function text(parent: Node, name: string): string {
  const v = parent[name];
  if (typeof v !== "string")
    throw new MarshalError(`missing ${name}`, undefined, name);
  return v;
}

function optText(parent: Node, name: string): string | undefined {
  const v = parent[name];
  return typeof v === "string" ? v : undefined;
}

// many reads a repeated element, which ofx-js gives as an array when it
// occurs more than once and as the element itself when once.
function many(parent: Node, name: string): Node[] {
  const v = parent[name];
  if (v === undefined) return [];
  const list = Array.isArray(v) ? v : [v];
  if (!list.every(isNode))
    throw new MarshalError(`malformed ${name}`, undefined, name);
  return list;
}

function date(stamp: string): string {
  const m = /^(\d{4})(\d{2})(\d{2})/.exec(stamp);
  if (!m) throw new MarshalError(`malformed date ${stamp}`);
  return iso(m[1], m[2], m[3]);
}

function currency(el: Node, base: string): string {
  const cur = el["CURRENCY"];
  return isNode(cur) ? text(cur, "CURSYM") : base;
}

// The asset class each kind of security list entry states.
const CLASS_OF: Record<string, AssetClass> = {
  STOCKINFO: AssetClass.EQUITY,
  OPTINFO: AssetClass.OPTION,
  MFINFO: AssetClass.MUTUAL_FUND,
  DEBTINFO: AssetClass.FIXED_INCOME,
  OTHERINFO: AssetClass.SECURITY,
};

interface Security {
  description: string;
  assetClass: AssetClass;
  identifiers: Identifier[];
}

function secId(el: Node): { key: string; identifier: Identifier } {
  const id = node(el, "SECID");
  const value = text(id, "UNIQUEID");
  const type = text(id, "UNIQUEIDTYPE");
  const identifier = (() => {
    switch (type) {
      case "ISIN":
        return ident(IdentifierType.ISIN, value);
      case "CUSIP":
        return ident(IdentifierType.CUSIP, value);
      case "SEDOL":
        return ident(IdentifierType.SEDOL, value);
      case "CONID":
        return ident(IdentifierType.BROKER_ID, value, "ibkr");
      default:
        throw new MarshalError(
          `unknown identifier type ${type}`,
          undefined,
          type,
        );
    }
  })();
  return { key: `${type}:${value}`, identifier };
}

function securities(ofx: Node): Map<string, Security> {
  const out = new Map<string, Security>();
  const list = node(node(ofx, "SECLISTMSGSRSV1"), "SECLIST");
  for (const [kind, assetClass] of Object.entries(CLASS_OF)) {
    for (const el of many(list, kind)) {
      const info = node(el, "SECINFO");
      const { key, identifier } = secId(info);
      const identifiers = [identifier];
      const occ =
        kind === "OPTINFO" ? optionOcc(el, text(info, "TICKER")) : undefined;
      if (occ) identifiers.push(ident(IdentifierType.OCC, occ));
      out.set(key, {
        description: text(info, "SECNAME"),
        assetClass,
        identifiers,
      });
    }
  }
  return out;
}

// optionOcc returns the OCC symbol an OPTINFO states, or undefined when its
// ticker is not in OCC form.
function optionOcc(el: Node, ticker: string): string | undefined {
  if (!isOcc(ticker)) return undefined;
  const built = buildOcc(
    ticker.slice(0, 6).trimEnd(),
    text(el, "DTEXPIRE"),
    text(el, "OPTTYPE"),
    text(el, "STRIKEPRICE"),
  );
  if (built !== undefined && built !== ticker) {
    throw new MarshalError(
      `option printed as ${ticker} but its terms name ${built}`,
    );
  }
  return built;
}

const SPLIT = /SPLIT (\d+) FOR (\d+)/;

function marshal(input: string): Upload {
  const ofx: unknown = parseSync(input).OFX;
  if (!isNode(ofx)) throw new MarshalError("not an OFX statement");
  const statement = node(
    node(node(ofx, "INVSTMTMSGSRSV1"), "INVSTMTTRNRS"),
    "INVSTMTRS",
  );
  const base = text(statement, "CURDEF");
  const list = node(statement, "INVTRANLIST");
  const known = securities(ofx);
  const rows: Row[] = [];
  const splits: StatedSplit[] = [];

  const keyOf = (el: Node, cur?: string) => {
    const { key } = secId(el);
    const sec = known.get(key);
    if (!sec) throw new MarshalError(`security ${key} not in SECLIST`);
    return securityKey({ ...sec, currency: cur });
  };

  for (const kind of Object.keys(list)) {
    if (kind === "DTSTART" || kind === "DTEND") continue;
    for (const el of many(list, kind)) {
      if (kind.startsWith("BUY") || kind.startsWith("SELL")) {
        const inv = node(el, kind.startsWith("BUY") ? "INVBUY" : "INVSELL");
        const cur = currency(inv, base);
        const when = date(text(node(inv, "INVTRAN"), "DTTRADE"));
        rows.push(
          ...trade({
            key: keyOf(inv, cur),
            orderDate: when,
            settlementDate: when,
            units: text(inv, "UNITS"),
            net: text(inv, "TOTAL"),
            fee: optText(inv, "COMMISSION") ?? "0",
            tax: optText(inv, "TAXES") ?? "0",
            currency: cur,
          }),
        );
      } else if (kind === "INCOME") {
        const cur = currency(el, base);
        const when = date(text(node(el, "INVTRAN"), "DTTRADE"));
        rows.push(leg(cashKey(cur), when, when, text(el, "TOTAL"), cur));
      } else if (kind === "INVBANKTRAN") {
        const trn = node(el, "STMTTRN");
        const cur = currency(trn, base);
        const when = date(text(trn, "DTPOSTED"));
        rows.push(leg(cashKey(cur), when, when, text(trn, "TRNAMT"), cur));
      } else if (kind === "TRANSFER") {
        const tran = node(el, "INVTRAN");
        const when = date(text(tran, "DTTRADE"));
        const units = text(el, "UNITS");
        const m = SPLIT.exec(optText(tran, "MEMO") ?? "");
        if (m) {
          splits.push(split(keyOf(el), when, units, { from: m[2], to: m[1] }));
        } else {
          rows.push(leg(keyOf(el), when, when, units));
        }
      } else {
        throw new MarshalError(`unknown element ${kind}`, undefined, kind);
      }
    }
  }

  return upload(Broker.IBKR, rows, splits, {
    from: date(text(list, "DTSTART")),
    to: date(text(list, "DTEND")),
  });
}

// The export date is not needed: every row is as at its order date.
export const ibkrQfx = { marshal };
