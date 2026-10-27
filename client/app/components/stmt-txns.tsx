import { StatedKeyChips } from "@/app/components/identifier-chip";
import { MarkChip } from "@/app/components/mark-chip";
import { Section } from "@/app/components/section";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import type { Txn } from "@/gen/stmt/v1/stmt_pb";
import type { ResolutionItem } from "@/gen/type/v1/type_pb";
import { toFixed } from "@/lib/marshal/decimal";

// StmtTxns lists a statement's transactions by order date, each
// with its key and, beside it, how firmly that key names an instrument on
// its date. The Instrument cell is empty when the statement lists no
// matching key.
export function StmtTxns({
  txns,
  keys,
  live,
}: {
  txns: Txn[];
  keys: ResolutionItem[];
  live: boolean;
}) {
  const byId = new Map(keys.map((k) => [k.statedKeyId, k]));
  return (
    <Section title="Transactions">
      <TableCard testId="stmt-txns" fixed>
        <colgroup>
          <col className="w-32" />
          <col />
          <col className="w-32" />
          <col className="w-32" />
        </colgroup>
        <Thead>
          <tr>
            <Th>Date</Th>
            <Th>Instrument</Th>
            <Th> </Th>
            <Th numeric>Quantity</Th>
          </tr>
        </Thead>
        <tbody>
          {txns.map((t) => (
            <Tr key={t.id} data-testid={`txn-row-${t.id}`}>
              <Td className="font-mono tabular-nums">{t.orderDate}</Td>
              <Td>
                <StatedKeyChips
                  statedKey={byId.get(t.statedKeyId)?.statedKey}
                />
              </Td>
              <Td>
                <MarkChip mark={t.mark} live={live} />
              </Td>
              <Td numeric>{toFixed(t.quantity, 2)}</Td>
            </Tr>
          ))}
        </tbody>
      </TableCard>
    </Section>
  );
}
