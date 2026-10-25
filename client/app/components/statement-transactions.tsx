import type { ReactNode } from "react";
import { StatedKeyChips } from "@/app/components/identifier-chip";
import { MarkChip } from "@/app/components/mark-chip";
import { Section } from "@/app/components/section";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import type { Transaction } from "@/gen/statement/v1/statement_pb";
import type { ResolutionItem } from "@/gen/type/v1/type_pb";
import { toFixed } from "@/lib/marshal/decimal";

// StatementTransactions lists a statement's transactions by order date, each
// with its key and how firmly that key names an instrument on its date. The
// Instrument cell is empty when the statement lists no matching key.
export function StatementTransactions({
  transactions,
  keys,
  live,
  action,
}: {
  transactions: Transaction[];
  keys: ResolutionItem[];
  live: boolean;
  // action renders the control a transaction offers, in a column of its own.
  action?: (t: Transaction, key: ResolutionItem | undefined) => ReactNode;
}) {
  const byId = new Map(keys.map((k) => [k.statedKeyId, k]));
  return (
    <Section title="Transactions">
      <TableCard testId="statement-transactions" fixed>
        <colgroup>
          <col className="w-32" />
          <col />
          <col className="w-32" />
          <col className="w-32" />
          {action && <col className="w-28" />}
        </colgroup>
        <Thead>
          <tr>
            <Th>Date</Th>
            <Th>Instrument</Th>
            <Th numeric>Quantity</Th>
            <Th>Mark</Th>
            {action && <Th> </Th>}
          </tr>
        </Thead>
        <tbody>
          {transactions.map((t) => (
            <Tr key={t.id} data-testid={`transaction-row-${t.id}`}>
              <Td className="font-mono tabular-nums">{t.orderDate}</Td>
              <Td>
                <StatedKeyChips
                  statedKey={byId.get(t.statedKeyId)?.statedKey}
                />
              </Td>
              <Td numeric>{toFixed(t.quantity, 2)}</Td>
              <Td>
                <MarkChip mark={t.mark} live={live} />
              </Td>
              {action && <Td>{action(t, byId.get(t.statedKeyId))}</Td>}
            </Tr>
          ))}
        </tbody>
      </TableCard>
    </Section>
  );
}
