"use client";

import {
  closestCenter,
  DndContext,
  type DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical } from "lucide-react";
import { useState } from "react";
import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { Dialog } from "@/app/components/dialog";
import { EmptyState } from "@/app/components/empty-state";
import { Input } from "@/app/components/input";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import type { Datasource } from "@/gen/admin/v1/admin_pb";
import {
  useDatasources,
  useReorderDatasources,
  useUpdateDatasource,
} from "@/hooks/use-datasources";
import { refusal } from "@/lib/refusal";

// The datasources this instance has registered, in precedence order, the
// first consulted first. A change takes effect at once.
export default function DatasourcesPage() {
  const { data, isPending, isError, refetch } = useDatasources();
  const update = useUpdateDatasource();
  const reorder = useReorderDatasources();
  const [editing, setEditing] = useState<Datasource | undefined>();
  const sources = data?.datasources ?? [];
  const busy = update.isPending || reorder.isPending;
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const from = sources.findIndex((d) => d.name === active.id);
    const to = sources.findIndex((d) => d.name === over.id);
    reorder.mutate(arrayMove(sources, from, to).map((d) => d.name));
  };

  return (
    <Page title="Datasources" width="wide" testId="admin-datasources-page">
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The datasources could not be loaded.
        </Notice>
      )}
      {update.isError && !editing && (
        <Notice tone="error" testId="datasource-update-error">
          {refusal(update.error, "The datasource could not be changed.")}
        </Notice>
      )}
      {reorder.isError && (
        <Notice tone="error" testId="datasource-reorder-error">
          The datasources could not be reordered.
        </Notice>
      )}
      {!isError && data && sources.length === 0 && (
        <EmptyState message="No datasources are registered." />
      )}
      {!isError && (isPending || sources.length > 0) && (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={onDragEnd}
        >
          <SortableContext
            items={sources.map((d) => d.name)}
            strategy={verticalListSortingStrategy}
          >
            <TableCard testId="admin-datasources-table">
              <Thead>
                <tr>
                  <Th aria-label="Order" />
                  <Th numeric>Precedence</Th>
                  <Th>Name</Th>
                  <Th>State</Th>
                  <Th>Endpoint</Th>
                  <Th>Credential</Th>
                  <Th aria-label="Actions" />
                </tr>
              </Thead>
              {isPending ? (
                <SkeletonRows columns={7} />
              ) : (
                <tbody>
                  {sources.map((d) => (
                    <Row
                      key={d.name}
                      d={d}
                      busy={busy}
                      onToggle={() =>
                        update.mutate({
                          name: d.name,
                          enabled: !d.enabled,
                          endpoint: d.endpoint,
                        })
                      }
                      onEdit={() => {
                        update.reset();
                        setEditing(d);
                      }}
                    />
                  ))}
                </tbody>
              )}
            </TableCard>
          </SortableContext>
        </DndContext>
      )}
      {editing && (
        <EditDialog
          d={editing}
          busy={update.isPending}
          error={update.error}
          onClose={() => {
            update.reset();
            setEditing(undefined);
          }}
          onSave={(endpoint, credential) =>
            update.mutate(
              {
                name: editing.name,
                enabled: editing.enabled,
                endpoint,
                credential,
              },
              { onSuccess: () => setEditing(undefined) },
            )
          }
        />
      )}
    </Page>
  );
}

function Row({
  d,
  busy,
  onToggle,
  onEdit,
}: {
  d: Datasource;
  busy: boolean;
  onToggle: () => void;
  onEdit: () => void;
}) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: d.name });
  return (
    <Tr
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
      }}
      data-testid={`datasource-row-${d.name}`}
    >
      <Td className="w-8">
        <button
          type="button"
          aria-label={`Drag to reorder ${d.name}`}
          data-testid={`datasource-grip-${d.name}`}
          className="cursor-grab touch-none rounded-md p-1 text-text-muted hover:text-text-primary active:cursor-grabbing"
          {...attributes}
          {...listeners}
        >
          <GripVertical aria-hidden className="h-4 w-4" />
        </button>
      </Td>
      <Td numeric data-testid={`datasource-precedence-${d.name}`}>
        {d.precedence}
      </Td>
      <Td>{d.name}</Td>
      <Td>
        <div className="flex items-center gap-3">
          <Chip tone={d.enabled ? "positive" : "muted"}>
            {d.enabled ? "enabled" : "disabled"}
          </Chip>
          <Button
            variant="secondary"
            data-testid={`datasource-toggle-${d.name}`}
            disabled={busy}
            onClick={onToggle}
          >
            {d.enabled ? "Disable" : "Enable"}
          </Button>
        </div>
      </Td>
      <Td className="font-mono">{d.endpoint}</Td>
      <Td>{d.hasCredential ? "held" : "none"}</Td>
      <Td>
        <Button
          variant="secondary"
          data-testid={`datasource-edit-${d.name}`}
          disabled={busy}
          onClick={onEdit}
        >
          Edit
        </Button>
      </Td>
    </Tr>
  );
}

// EditDialog edits the endpoint and the credential. The stored credential
// is never shown: a blank field keeps it, a value replaces it, and the
// clear box drops it.
function EditDialog({
  d,
  busy,
  error,
  onClose,
  onSave,
}: {
  d: Datasource;
  busy: boolean;
  // error is the failure of the last save. The dialog shows it and stays
  // open.
  error: Error | null;
  onClose: () => void;
  onSave: (endpoint: string, credential: string | undefined) => void;
}) {
  const [endpoint, setEndpoint] = useState(d.endpoint ?? "");
  const [credential, setCredential] = useState("");
  const [clear, setClear] = useState(false);
  return (
    <Dialog
      open
      onClose={onClose}
      title={`Edit ${d.name}`}
      testId="datasource-dialog"
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            data-testid="datasource-save"
            disabled={busy}
            onClick={() =>
              onSave(endpoint, clear ? "" : credential || undefined)
            }
          >
            Save
          </Button>
        </>
      }
    >
      {error && (
        <Notice tone="error" testId="datasource-dialog-error">
          {refusal(error, "The datasource could not be changed.")}
        </Notice>
      )}
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-text-muted">Endpoint</span>
        <Input
          type="url"
          data-testid="datasource-endpoint"
          value={endpoint}
          placeholder="the provider's default"
          onChange={(e) => setEndpoint(e.target.value)}
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-text-muted">
          Credential{d.hasCredential ? " (one is held)" : ""}
        </span>
        <Input
          type="password"
          data-testid="datasource-credential"
          value={credential}
          placeholder={d.hasCredential ? "unchanged" : "none"}
          disabled={clear}
          autoComplete="off"
          onChange={(e) => setCredential(e.target.value)}
        />
      </label>
      {d.hasCredential && (
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            data-testid="datasource-clear-credential"
            checked={clear}
            onChange={(e) => setClear(e.target.checked)}
          />
          <span>Clear the credential held</span>
        </label>
      )}
    </Dialog>
  );
}
