"use client";

import { useState } from "react";
import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Select } from "@/app/components/input";
import { Notice } from "@/app/components/notice";
import type { UserRun } from "@/gen/admin/v1/admin_pb";
import type { Run } from "@/gen/run/v1/run_pb";
import { useDatasources } from "@/hooks/use-datasources";
import { useStartReplay } from "@/hooks/use-replay";
import { runLabel } from "@/lib/admin";
import { refusal } from "@/lib/refusal";

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
  const label = runLabel(r);
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
          {refusal(start.error, "The replay could not be started.")}
        </Notice>
      )}
      <fieldset className="flex flex-col gap-2 text-sm">
        <legend className="mb-1 text-text-muted">Keys</legend>
        <label className="flex items-center gap-2">
          <input
            type="radio"
            name="scope"
            data-testid="replay-scope-unavailable"
            checked={scope === "unavailable"}
            onChange={() => setScope("unavailable")}
          />
          <span>Keys left unavailable</span>
        </label>
        <div className="flex items-center gap-2">
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name="scope"
              data-testid="replay-scope-datasource"
              checked={scope === "datasource"}
              onChange={() => setScope("datasource")}
            />
            <span>Keys a datasource has not yet answered</span>
          </label>
          <Select
            data-testid="replay-datasource"
            aria-label="Datasource"
            value={chosen}
            disabled={scope !== "datasource"}
            onChange={(e) => setDatasource(e.target.value)}
          >
            {enabled.map((d) => (
              <option key={d.name} value={d.name}>
                {d.name}
              </option>
            ))}
          </Select>
        </div>
      </fieldset>
    </Dialog>
  );
}
