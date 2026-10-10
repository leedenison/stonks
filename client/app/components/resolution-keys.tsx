import type { ReactNode } from "react";
import { Chip } from "@/app/components/chip";
import { StatedKeyChips } from "@/app/components/identifier-chip";
import { ResolutionChip } from "@/app/components/resolution-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import {
  AssetClass,
  Arbiter,
  type ResolutionItem,
} from "@/gen/type/v1/type_pb";
import { assetClassLabel } from "@/lib/asset-class";

// ResolutionKeys lists stated keys in a table per asset class, one row per
// key, with the outcome of resolving each. Every table lays its columns out
// at the same widths, so the tables read as one list. When the user has
// chosen an instrument for a key, its row says so beside the outcome.
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
    <div data-testid={testId} className="flex flex-col gap-4">
      {byClass(keys).map(([c, group]) => (
        <div key={c} className="flex flex-col gap-2">
          <h3 className="text-sm font-medium text-text-muted">
            {c === AssetClass.UNKNOWN ? "Unknown class" : assetClassLabel(c)}
          </h3>
          <TableCard testId={`${testId}-${AssetClass[c].toLowerCase()}`} fixed>
            <colgroup>
              <col className="w-1/2" />
              <col className="w-1/6" />
              <col />
              {action && <col className="w-28" />}
            </colgroup>
            <Thead>
              <tr>
                <Th>Description</Th>
                <Th>Outcome</Th>
                <Th>Reason</Th>
                {action && <Th> </Th>}
              </tr>
            </Thead>
            <tbody>
              {group.map((k) => (
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
        </div>
      ))}
    </div>
  );
}

// byClass groups keys by asset class in enum order, the unknown class
// last. A key whose class is unset counts as unknown.
function byClass(keys: ResolutionItem[]): [AssetClass, ResolutionItem[]][] {
  const groups = new Map<AssetClass, ResolutionItem[]>();
  for (const k of keys) {
    const c = k.statedKey?.assetClass || AssetClass.UNKNOWN;
    groups.set(c, [...(groups.get(c) ?? []), k]);
  }
  const rank = (c: AssetClass) => (c === AssetClass.UNKNOWN ? Infinity : c);
  return [...groups].sort(([a], [b]) => rank(a) - rank(b));
}
