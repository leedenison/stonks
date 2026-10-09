import type { ReactNode } from "react";
import { Chip } from "@/app/components/chip";
import { StatedKeyChips } from "@/app/components/identifier-chip";
import { ResolutionChip } from "@/app/components/resolution-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { Arbiter, type ResolutionItem } from "@/gen/type/v1/type_pb";

// ResolutionKeys lists stated keys, one row per key, with the outcome of
// resolving each. When the user has chosen an instrument for a key, its
// row says so beside the outcome.
export function ResolutionKeys({
  keys,
  live,
  testId,
  rowTestId,
  action,
}: {
  keys: ResolutionItem[];
  // live is true while the run resolving the keys is still running. A key
  // with no outcome then reads as pending.
  live: boolean;
  testId: string;
  rowTestId: (statedKeyId: string) => string;
  // action renders the control a key offers, in a column of its own.
  action?: (k: ResolutionItem) => ReactNode;
}) {
  return (
    <TableCard testId={testId}>
      <Thead>
        <tr>
          <Th>Stated key</Th>
          <Th>Outcome</Th>
          <Th>Reason</Th>
          {action && <Th> </Th>}
        </tr>
      </Thead>
      <tbody>
        {keys.map((k) => (
          <Tr key={k.statedKeyId} data-testid={rowTestId(k.statedKeyId)}>
            <Td>
              <StatedKeyChips statedKey={k.statedKey} />
            </Td>
            <Td>
              <span className="flex flex-wrap gap-1">
                <ResolutionChip outcome={k.outcome} live={live} />
                {k.arbiter === Arbiter.USER && (
                  <Chip
                    tone="positive"
                    title="You chose the instrument"
                    data-testid={`key-arbiter-${k.statedKeyId}`}
                    data-state="user"
                  >
                    Chosen
                  </Chip>
                )}
              </span>
            </Td>
            <Td>
              <ul>
                {k.reasons.map((r, i) => (
                  <li key={i}>{r}</li>
                ))}
              </ul>
            </Td>
            {action && <Td>{action(k)}</Td>}
          </Tr>
        ))}
      </tbody>
    </TableCard>
  );
}
