import { StatedKeyChips } from "@/app/components/identifier-chip";
import { ResolutionChip } from "@/app/components/resolution-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import type { ResolutionItem } from "@/gen/type/v1/type_pb";

// ResolutionKeys lists stated keys, one row per key, with the outcome of
// resolving each.
export function ResolutionKeys({
  keys,
  live,
  testId,
  rowTestId,
}: {
  keys: ResolutionItem[];
  // live is true while the run resolving the keys is still running. A key
  // with no outcome then reads as pending.
  live: boolean;
  testId: string;
  rowTestId: (statedKeyId: string) => string;
}) {
  return (
    <TableCard testId={testId}>
      <Thead>
        <tr>
          <Th>Stated key</Th>
          <Th>Outcome</Th>
          <Th>Reason</Th>
        </tr>
      </Thead>
      <tbody>
        {keys.map((k) => (
          <Tr key={k.statedKeyId} data-testid={rowTestId(k.statedKeyId)}>
            <Td>
              <StatedKeyChips statedKey={k.statedKey} />
            </Td>
            <Td>
              <ResolutionChip outcome={k.outcome} live={live} />
            </Td>
            <Td>
              <Reason text={k.reason} />
            </Td>
          </Tr>
        ))}
      </tbody>
    </TableCard>
  );
}

// Reason lists the parts of a resolution reason. The server joins one part
// per datasource with semicolons.
function Reason({ text }: { text?: string }) {
  const parts = (text ?? "")
    .split(";")
    .map((p) => p.trim())
    .filter(Boolean);
  return (
    <ul>
      {parts.map((p, i) => (
        <li key={i}>{p}</li>
      ))}
    </ul>
  );
}
