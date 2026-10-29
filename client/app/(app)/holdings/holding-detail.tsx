import { IdentifierChips } from "@/app/components/identifier-chip";
import { Td } from "@/app/components/table";
import { type DayRange } from "@/lib/day-range";
import { type HoldingRow } from "@/lib/holdings";

// HoldingDetail is the row that opens beneath a holding. It shows the
// holding's registry codes and the brokers' descriptions of it. For a
// provisional holding it also shows the days with no known corporate actions.
export function HoldingDetail({
  row,
  columns,
}: {
  row: HoldingRow;
  // The number of table columns the detail row spans.
  columns: number;
}) {
  return (
    <tr data-testid={`holding-detail-${row.id}`}>
      <Td colSpan={columns} className="bg-surface-tint pl-8">
        <div className="flex animate-fade-in flex-wrap gap-8 text-xs">
          {row.identifiers.length > 0 && (
            <Part title="Identifiers:">
              <IdentifierChips ids={row.identifiers} />
            </Part>
          )}
          {row.descriptions.length > 0 && (
            <Part title="Descriptions:">
              <ul
                className="flex flex-wrap gap-x-4 gap-y-1"
                data-testid={`holding-descriptions-${row.id}`}
              >
                {row.descriptions.map((d) => (
                  <li key={d}>{d}</li>
                ))}
              </ul>
            </Part>
          )}
          {row.provisional && (
            <Part title="Provisional:">
              <div data-testid={`holding-provisional-${row.id}`}>
                <p>
                  The quantity follows every corporate action known so far. Some
                  days from{" "}
                  <span className="font-mono tabular-nums">
                    {row.provisionalFrom}
                  </span>{" "}
                  to today have no known corporate actions, so an action may be
                  missing.
                </p>
                <ul>
                  {row.gaps.map((g) => (
                    <li key={g.from}>{gapText(g)}</li>
                  ))}
                </ul>
              </div>
            </Part>
          )}
        </div>
      </Td>
    </tr>
  );
}

function Part({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="font-semibold tracking-wider text-text-muted uppercase">
        {title}
      </span>
      {children}
    </div>
  );
}

// gapText words a gap for display.
function gapText(g: DayRange): string {
  return g.from === g.to
    ? `Corporate actions are unknown for ${g.from}.`
    : `Corporate actions are unknown for ${g.from} to ${g.to}.`;
}
