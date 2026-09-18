// SkeletonRows stands in for a table's body while it loads, shaped like the
// rows it will hold so nothing shifts when they arrive.
export function SkeletonRows({
  columns,
  rows = 4,
}: {
  columns: number;
  rows?: number;
}) {
  return (
    <tbody data-testid="skeleton-rows" aria-busy="true">
      {Array.from({ length: rows }, (_, r) => (
        <tr key={r}>
          {Array.from({ length: columns }, (_, c) => (
            <td key={c} className="border-b border-border px-4 py-3">
              <div
                className="h-4 animate-pulse rounded bg-border"
                style={{ width: `${55 + ((r + c) % 3) * 15}%` }}
              />
            </td>
          ))}
        </tr>
      ))}
    </tbody>
  );
}
