"use client";

import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { useDatasources } from "@/hooks/use-datasources";

// The datasources this instance has registered, in precedence order. The
// registry is read at startup, so a change here takes a restart.
export default function DatasourcesPage() {
  const { data, isPending, isError, refetch } = useDatasources();
  const sources = data?.datasources ?? [];

  return (
    <Page title="Datasources" width="wide" testId="admin-datasources-page">
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The datasources could not be loaded.
        </Notice>
      )}
      {!isError && data && sources.length === 0 && (
        <EmptyState message="No datasources are registered." />
      )}
      {!isError && (isPending || sources.length > 0) && (
        <TableCard testId="admin-datasources-table">
          <Thead>
            <tr>
              <Th numeric>Precedence</Th>
              <Th>Name</Th>
              <Th>State</Th>
              <Th>Endpoint</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={4} />
          ) : (
            <tbody>
              {sources.map((d) => (
                <Tr key={d.name} data-testid={`datasource-row-${d.name}`}>
                  <Td numeric>{d.precedence}</Td>
                  <Td>{d.name}</Td>
                  <Td>
                    <Chip tone={d.enabled ? "positive" : "muted"}>
                      {d.enabled ? "enabled" : "disabled"}
                    </Chip>
                  </Td>
                  <Td className="font-mono">{d.endpoint}</Td>
                </Tr>
              ))}
            </tbody>
          )}
        </TableCard>
      )}
    </Page>
  );
}
