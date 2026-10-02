"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useState } from "react";
import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import type { UserRun } from "@/gen/admin/v1/admin_pb";
import type { Run } from "@/gen/run/v1/run_pb";
import { useDatasources } from "@/hooks/use-datasources";
import { useStartReplay } from "@/hooks/use-replay";
import { enumLabel, runEnums } from "@/lib/admin";
import { formatInstant } from "@/lib/format";

const selectClass = "rounded-md border border-border bg-surface px-2 py-1";

// ReplayDialog starts a replay over the keys of run: those left unavailable,
// or those an enabled datasource has not yet answered. A refusal is shown in
// the dialog, which stays open; a start hands the run to onStarted.
export function ReplayDialog({
  run,
  onClose,
  onStarted,
}: {
  run: UserRun;
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const { data } = useDatasources();
  const start = useStartReplay();
  const enabled = (data?.datasources ?? []).filter((d) => d.enabled);
  const [scope, setScope] = useState<"unavailable" | "datasource">(
    "unavailable",
  );
  const [datasource, setDatasource] = useState("");
  const chosen = datasource || enabled[0]?.name || "";
  const r = run.run;
  const label = r
    ? `${enumLabel(runEnums.kind, r.kind)} run${r.createdAt ? ` @ ${formatInstant(r.createdAt)}` : ""}`
    : "run";
  const submit = () =>
    start.mutate(
      {
        runId: r?.id ?? "",
        scope:
          scope === "unavailable"
            ? { case: "unavailable", value: true }
            : { case: "datasource", value: chosen },
      },
      { onSuccess: (res) => res.run && onStarted(res.run) },
    );

  return (
    <Dialog
      open
      onClose={onClose}
      title="Replay"
      testId="replay-dialog"
      footer={
        <>
          <Button
            variant="secondary"
            onClick={onClose}
            disabled={start.isPending}
          >
            Cancel
          </Button>
          <Button
            data-testid="replay-start"
            disabled={start.isPending || (scope === "datasource" && !chosen)}
            onClick={submit}
          >
            Start
          </Button>
        </>
      }
    >
      <p className="text-sm">
        Re-resolve the keys of the {label} for {run.userEmail}.
      </p>
      {start.isError && (
        <Notice tone="error" testId="replay-error">
          {refusal(start.error)}
        </Notice>
      )}
      <label className="flex items-center gap-2 text-sm">
        <input
          type="radio"
          name="scope"
          data-testid="replay-scope-unavailable"
          checked={scope === "unavailable"}
          onChange={() => setScope("unavailable")}
        />
        <span>Keys left unavailable</span>
      </label>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="radio"
          name="scope"
          data-testid="replay-scope-datasource"
          checked={scope === "datasource"}
          onChange={() => setScope("datasource")}
        />
        <span>Keys a datasource has not yet answered</span>
        <select
          data-testid="replay-datasource"
          className={selectClass}
          value={chosen}
          disabled={scope !== "datasource"}
          onChange={(e) => setDatasource(e.target.value)}
        >
          {enabled.map((d) => (
            <option key={d.name} value={d.name}>
              {d.name}
            </option>
          ))}
        </select>
      </label>
    </Dialog>
  );
}

// refusal is the message of a refused start, which carries the service's
// reason, and a fixed line for any other failure.
function refusal(error: Error | null): string {
  if (error instanceof ConnectError && error.code === Code.FailedPrecondition) {
    return error.rawMessage;
  }
  return "The replay could not be started.";
}
