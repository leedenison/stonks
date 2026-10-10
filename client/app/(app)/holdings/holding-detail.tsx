import { IdentifierChips } from "@/app/components/identifier-chip";
import { Td } from "@/app/components/table";
import { type HoldingRow } from "@/lib/holdings";

// HoldingDetail is the row that opens beneath a holding. It shows the
// holding's registry codes and the brokers' descriptions of it.
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
