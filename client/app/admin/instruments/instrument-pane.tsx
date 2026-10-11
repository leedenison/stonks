import { Chip } from "@/app/components/chip";
import { identifierLabel } from "@/app/components/identifier-chip";
import { Section } from "@/app/components/section";
import { Td, Th } from "@/app/components/table";
import type { Instrument } from "@/gen/admin/instrument/v1/instrument_pb";
import { type Identifier, IdentifierType } from "@/gen/type/v1/type_pb";
import { assetClassLabel } from "@/lib/asset-class";
import { groupIdentifiers, identifierKey } from "@/lib/identifiers";

// InstrumentPane shows one instrument in full, in sections. The identifier
// naming the instrument and each listing's primary venue are marked in the
// accent tone.
export function InstrumentPane({ instrument }: { instrument: Instrument }) {
  const marks = {
    via: new Set(instrument.via.map(identifierKey)),
    name: new Set(instrument.name ? [identifierKey(instrument.name)] : []),
  };
  return (
    <div
      data-testid="instrument-pane"
      className="flex min-w-0 flex-1 flex-col gap-6 rounded-md border border-border bg-surface px-5 py-4 shadow-xs"
    >
      <div className="flex flex-wrap items-baseline gap-3">
        <h2 className="text-xl font-semibold tracking-tight">
          {instrument.name?.value || instrument.id}
        </h2>
        <Chip>{assetClassLabel(instrument.assetClass)}</Chip>
        <span className="font-mono text-xs text-text-muted">
          {instrument.id}
        </span>
      </div>
      <Section title="Identifiers">
        <Grouped
          ids={instrument.identifiers}
          marks={marks}
          valueHead="Value"
          testId="instrument-identifiers"
        />
      </Section>
      <Section title="Listings">
        {instrument.listings.length === 0 && <None />}
        {instrument.listings.map((l) => (
          <div
            key={l.id}
            data-testid={`listing-${l.id}`}
            className="flex flex-col gap-1"
          >
            <div className="flex items-baseline gap-3 text-sm">
              <span className="font-mono font-semibold tabular-nums">
                {l.currency}
              </span>
              {l.primaryMic && (
                <span className="text-text-muted">
                  Primary <span className="font-mono">{l.primaryMic}</span>
                </span>
              )}
            </div>
            <Grouped
              ids={l.identifiers}
              marks={marks}
              primary={l.primaryMic}
              valueHead="Symbol"
            />
          </div>
        ))}
      </Section>
    </div>
  );
}

function None() {
  return <p className="text-sm text-text-muted">None.</p>;
}

// Grouped lists identifiers by type, one row per value with its domains.
function Grouped({
  ids,
  marks,
  primary = "",
  valueHead,
  testId,
}: {
  ids: Identifier[];
  marks: Parameters<typeof groupIdentifiers>[1];
  // The domain to mark as the primary venue; empty for none.
  primary?: string;
  valueHead: string;
  testId?: string;
}) {
  if (ids.length === 0) {
    return <None />;
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
  return (
    <table
      data-testid={testId}
      className="w-full table-fixed border-collapse border-b border-border text-sm"
    >
      <colgroup>
        <col className="w-1/5" />
        <col className="w-1/5" />
        <col />
      </colgroup>
      <thead className="border-b border-border">
        <tr>
          <Th dense>Type</Th>
          <Th dense>{valueHead}</Th>
          <Th dense>Domain</Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
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
                  <Chip
                    key={d}
                    tone={d === primary ? "accent" : "muted"}
                    data-primary={d === primary ? "true" : undefined}
                    className="font-mono"
                  >
                    {d}
                  </Chip>
                ))}
              </span>
            </Td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
