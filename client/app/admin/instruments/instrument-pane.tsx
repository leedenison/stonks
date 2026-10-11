import { Chip } from "@/app/components/chip";
import { identifierLabel } from "@/app/components/identifier-chip";
import { Section } from "@/app/components/section";
import { Td, Th } from "@/app/components/table";
import type { Instrument } from "@/gen/admin/instrument/v1/instrument_pb";
import { type Identifier, IdentifierType } from "@/gen/type/v1/type_pb";
import { assetClassLabel } from "@/lib/asset-class";
import { groupIdentifiers } from "@/lib/identifiers";

// InstrumentPane shows one instrument in full, in sections.
export function InstrumentPane({ instrument }: { instrument: Instrument }) {
  return (
    <div
      data-testid="instrument-pane"
      className="flex min-w-0 flex-1 flex-col gap-6 rounded-md border border-border bg-surface px-5 py-4 shadow-xs"
    >
      <div className="flex flex-wrap items-baseline gap-3">
        <h2 className="text-xl font-semibold tracking-tight">
          {instrument.name || instrument.id}
        </h2>
        <Chip>{assetClassLabel(instrument.assetClass)}</Chip>
        <span className="font-mono text-xs text-text-muted">
          {instrument.id}
        </span>
      </div>
      <Section title="Identifiers">
        <Grouped
          ids={instrument.identifiers}
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
            <Grouped ids={l.identifiers} valueHead="Symbol" />
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
  valueHead,
  testId,
}: {
  ids: Identifier[];
  valueHead: string;
  testId?: string;
}) {
  if (ids.length === 0) {
    return <None />;
  }
  const groups = groupIdentifiers(ids);
  const span = new Map<IdentifierType, number>();
  for (const g of groups) {
    span.set(g.type, (span.get(g.type) ?? 0) + 1);
  }
  const rows = groups.map((g, i) => ({
    ...g,
    first: i === 0 || groups[i - 1].type !== g.type,
  }));
  return (
    <table data-testid={testId} className="w-full border-collapse text-sm">
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
            <Td dense className="font-mono">
              {r.value}
            </Td>
            <Td dense className="font-mono">
              {r.domains.join(", ")}
            </Td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
