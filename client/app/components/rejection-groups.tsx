import { ChevronRight } from "lucide-react";
import type { StatementItem } from "@/gen/statement/v1/statement_pb";
import { groupByReason, keyLabel } from "@/lib/rejections";
import { Chip } from "./chip";
import { Td, Th } from "./table";

// RejectionGroups lists rejected rows by reason. Each reason is a line with
// its count that opens to the rows it covers; a lone reason starts open.
// Without rows, as in a narrow column, each reason is the line alone.
export function RejectionGroups({
  items,
  rows = true,
}: {
  items: StatementItem[];
  rows?: boolean;
}) {
  const groups = groupByReason(items);
  if (!rows) {
    return (
      <ul data-testid="rejection-groups" className="flex flex-col gap-1">
        {groups.map((g, i) => (
          <li
            key={g.reason}
            data-testid={`rejection-group-${i}`}
            className="flex items-start gap-2 text-sm"
          >
            <span
              data-testid={`rejection-reason-${i}`}
              className="min-w-0 flex-1 break-words text-text-primary"
            >
              {g.reason}
            </span>
            <Chip tone="accent" data-testid={`rejection-count-${i}`}>
              {g.items.length}
            </Chip>
          </li>
        ))}
      </ul>
    );
  }
  return (
    <div data-testid="rejection-groups" className="flex flex-col gap-2">
      {groups.map((g, i) => (
        <details
          key={g.reason}
          data-testid={`rejection-group-${i}`}
          open={groups.length === 1}
          className="group rounded-md border border-border bg-surface"
        >
          <summary className="flex cursor-pointer items-center gap-2 px-4 py-2.5 text-sm select-none hover:bg-primary-light/15">
            <ChevronRight
              aria-hidden
              className="h-4 w-4 shrink-0 text-text-muted transition-transform group-open:rotate-90"
            />
            <span
              data-testid={`rejection-reason-${i}`}
              className="flex-1 font-medium text-text-primary"
            >
              {g.reason}
            </span>
            <Chip tone="accent" data-testid={`rejection-count-${i}`}>
              {g.items.length}
            </Chip>
          </summary>
          <table className="w-full border-t border-border text-sm">
            <thead>
              <tr>
                <Th dense numeric>
                  Row
                </Th>
                <Th dense className="whitespace-nowrap">
                  Order date
                </Th>
                <Th dense className="w-full">
                  Key
                </Th>
                <Th dense numeric>
                  Quantity
                </Th>
              </tr>
            </thead>
            <tbody>
              {g.items.map((item) => (
                <tr
                  key={item.ordinal}
                  data-testid={`rejection-row-${item.ordinal}`}
                  className="border-t border-border"
                >
                  <Td dense numeric className="text-text-muted">
                    {item.ordinal}
                  </Td>
                  <Td
                    dense
                    className="font-mono whitespace-nowrap tabular-nums"
                  >
                    {item.row?.orderDate}
                  </Td>
                  <Td dense className="break-words">
                    {keyLabel(item.row?.key)}
                  </Td>
                  <Td dense numeric className="whitespace-nowrap">
                    {item.row?.quantity}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </details>
      ))}
    </div>
  );
}
