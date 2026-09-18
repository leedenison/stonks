import { ChevronRight } from "lucide-react";
import type { StatementItem } from "@/gen/statement/v1/statement_pb";
import { groupByReason, keyLabel } from "@/lib/rejections";
import { Chip } from "./chip";

// RejectionGroups lists rejected rows by reason. Each reason is a line with
// its count that opens to the rows it covers; a lone reason starts open.
export function RejectionGroups({ items }: { items: StatementItem[] }) {
  const groups = groupByReason(items);
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
              <tr className="text-xs font-semibold tracking-wider text-text-muted uppercase">
                <th className="px-4 py-2 text-right">Row</th>
                <th className="px-4 py-2 text-left">Order date</th>
                <th className="px-4 py-2 text-left">Key</th>
                <th className="px-4 py-2 text-right">Quantity</th>
              </tr>
            </thead>
            <tbody>
              {g.items.map((item) => (
                <tr
                  key={item.ordinal}
                  data-testid={`rejection-row-${item.ordinal}`}
                  className="border-t border-border"
                >
                  <td className="px-4 py-2 text-right font-mono tabular-nums text-text-muted">
                    {item.ordinal}
                  </td>
                  <td className="px-4 py-2 font-mono tabular-nums">
                    {item.row?.orderDate}
                  </td>
                  <td className="px-4 py-2">{keyLabel(item.row?.key)}</td>
                  <td className="px-4 py-2 text-right font-mono tabular-nums">
                    {item.row?.quantity}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </details>
      ))}
    </div>
  );
}
