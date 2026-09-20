"use client";

import { FileUp, LoaderCircle } from "lucide-react";
import { type ChangeEvent, useEffect, useMemo, useState } from "react";
import type { Run } from "@/gen/run/v1/run_pb";
import type { Statement } from "@/gen/statement/v1/statement_pb";
import { Broker } from "@/gen/type/v1/type_pb";
import { useCreateStatement } from "@/hooks/use-create-statement";
import { useDropTarget } from "@/hooks/use-drop-target";
import { brokerLabel, brokers } from "@/lib/broker";
import { formatQuantity } from "@/lib/format";
import { today } from "@/lib/marshal/date";
import { recognisedBy } from "@/lib/marshal/marshal";
import { keyLabel } from "@/lib/rejections";
import { needsExportDate, parseExport } from "@/lib/upload/parse";
import { claim, type Period, period } from "@/lib/upload/period";
import { refusal } from "@/lib/upload/refusal";
import { Button } from "./button";
import { Dialog } from "./dialog";
import { Notice } from "./notice";
import { Td, Th } from "./table";

// The largest file taken, in bytes. A broker export is kilobytes; anything
// larger is not one.
const maxBytes = 5 * 1024 * 1024;

const inputClass =
  "rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-text-primary focus:border-primary focus:ring-1 focus:ring-primary/30 focus:outline-hidden";

type Loaded = { name: string; type: string; text: string };

const oversize = (file: File) => file.size > maxBytes;
const tooLarge = (file: File) => `${file.name} is larger than a broker export.`;
const unreadable = (file: File) => `${file.name} could not be read.`;

// UploadDialog takes a broker's export through its stages: choose or drop a
// file, read it, review what the marshaller made of it with the broker,
// the export date and the period open to change, and submit. The
// dialog closes once the run is created and hands it to onCreated.
export function UploadDialog({
  open,
  initial,
  onClose,
  onCreated,
}: {
  open: boolean;
  initial?: File;
  onClose: () => void;
  onCreated?: (run: Run) => void;
}) {
  const [reading, setReading] = useState(
    initial !== undefined && !oversize(initial),
  );
  const [loaded, setLoaded] = useState<Loaded>();
  const [fileError, setFileError] = useState(() =>
    initial && oversize(initial) ? tooLarge(initial) : undefined,
  );
  const [broker, setBroker] = useState<Broker>();
  const [guesses, setGuesses] = useState<Broker[]>([]);
  const [exportedOn, setExportedOn] = useState(today);
  const [chosen, setChosen] = useState<Period>();
  const created = useCreateStatement();

  const finish = (file: File, text: string) => {
    const found = recognisedBy(text, file.type);
    setLoaded({ name: file.name, type: file.type, text });
    setGuesses(found);
    setBroker(found.length === 1 ? found[0] : undefined);
    setChosen(undefined);
    setReading(false);
  };

  const refuse = (message: string) => {
    setFileError(message);
    setReading(false);
  };

  // An oversize file is refused on its size, before any of it is read.
  const take = (file: File) => {
    if (oversize(file)) {
      setFileError(tooLarge(file));
      return;
    }
    setFileError(undefined);
    setReading(true);
    file.text().then(
      (text) => finish(file, text),
      () => refuse(unreadable(file)),
    );
  };

  // A file handed over at opening is read at once, unless its size already
  // refused it. The read is started here and settles after this opening,
  // unless the dialog has gone by then.
  useEffect(() => {
    if (!initial || oversize(initial)) {
      return;
    }
    let live = true;
    initial.text().then(
      (text) => {
        if (live) {
          finish(initial, text);
        }
      },
      () => {
        if (live) {
          refuse(unreadable(initial));
        }
      },
    );
    return () => {
      live = false;
    };
  }, [initial]);

  const parsed = useMemo(
    () =>
      loaded && broker !== undefined
        ? parseExport(loaded.text, broker, exportedOn)
        : undefined,
    [loaded, broker, exportedOn],
  );
  const statement = parsed?.statement;
  const span = statement ? period(statement, chosen) : undefined;

  const submit = () => {
    if (!statement || !span) {
      return;
    }
    created.mutate(claim(statement, span), {
      onSuccess: (res) => {
        onClose();
        if (res.run) {
          onCreated?.(res.run);
        }
      },
    });
  };

  const back = () => {
    setLoaded(undefined);
    setBroker(undefined);
    setGuesses([]);
    setChosen(undefined);
    created.reset();
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Upload a statement"
      testId="upload-dialog"
      footer={
        !reading && loaded ? (
          <>
            <Button
              variant="secondary"
              data-testid="upload-back"
              onClick={back}
            >
              Choose another file
            </Button>
            <Button
              data-testid="upload-submit"
              disabled={!span?.valid || created.isPending}
              onClick={submit}
            >
              {created.isPending ? "Uploading" : "Upload"}
            </Button>
          </>
        ) : undefined
      }
    >
      {reading && (
        <p className="flex items-center gap-2 text-sm text-text-muted">
          <LoaderCircle aria-hidden className="h-4 w-4 animate-spin" />
          Reading the export
        </p>
      )}
      {!reading && !loaded && <Choose onFile={take} error={fileError} />}
      {!reading && loaded && (
        <div className="flex flex-col gap-4">
          <p
            data-testid="upload-recognised"
            className="text-sm text-text-muted"
          >
            <span className="font-medium text-text-primary">{loaded.name}</span>
            {" - "}
            {guesses.length === 0
              ? "Not recognised: choose the broker"
              : `Recognised as: ${guesses.map(brokerLabel).join(" or ")} export`}
          </p>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-text-muted">Broker</span>
            <select
              data-testid="upload-broker"
              className={inputClass}
              value={broker ?? ""}
              onChange={(e) => {
                setBroker(
                  e.target.value === "" ? undefined : Number(e.target.value),
                );
                setChosen(undefined);
                created.reset();
              }}
            >
              <option value="">Choose a broker</option>
              {brokers.map((b) => (
                <option key={b} value={b}>
                  {brokerLabel(b)}
                </option>
              ))}
            </select>
          </label>
          {broker !== undefined && needsExportDate(broker) && (
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-text-muted">Date the export was taken</span>
              <input
                type="date"
                data-testid="upload-exported-on"
                className={inputClass}
                value={exportedOn}
                onChange={(e) => setExportedOn(e.target.value)}
              />
            </label>
          )}
          {parsed?.error && (
            <Notice tone="error" testId="upload-error">
              {parsed.error.message}
            </Notice>
          )}
          {statement && span && (
            <>
              <p data-testid="upload-rows" className="text-sm">
                <span className="font-mono tabular-nums">
                  {statement.rows.length}
                </span>{" "}
                rows
                {statement.splits.length > 0 &&
                  ` and ${statement.splits.length} stated splits`}
              </p>
              <div className="flex flex-wrap gap-4 text-sm">
                <label className="flex flex-col gap-1">
                  <span className="text-text-muted">Period from</span>
                  <input
                    type="date"
                    data-testid="upload-from"
                    className={inputClass}
                    value={span.from}
                    onChange={(e) =>
                      setChosen({ from: e.target.value, to: span.to })
                    }
                  />
                </label>
                <label className="flex flex-col gap-1">
                  <span className="text-text-muted">Period to</span>
                  <input
                    type="date"
                    data-testid="upload-to"
                    className={inputClass}
                    value={span.to}
                    onChange={(e) =>
                      setChosen({ from: span.from, to: e.target.value })
                    }
                  />
                </label>
              </div>
              {span.outside > 0 && (
                <Notice testId="upload-outside">
                  {span.outside} of the rows fall outside the period and will be
                  rejected.
                </Notice>
              )}
              {!span.valid && (
                <Notice tone="error">
                  The period must start before it ends.
                </Notice>
              )}
              <Preview statement={statement} />
            </>
          )}
          {created.error && (
            <Notice tone="error" testId="upload-refused">
              {refusal(created.error)}
            </Notice>
          )}
        </div>
      )}
    </Dialog>
  );
}

function Choose({
  onFile,
  error,
}: {
  onFile: (file: File) => void;
  error?: string;
}) {
  const drop = useDropTarget(onFile);
  const onChange = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      onFile(file);
    }
  };
  return (
    <div className="flex flex-col gap-3">
      <label
        data-testid="upload-drop"
        {...drop}
        className="flex cursor-pointer flex-col items-center gap-2 rounded-md border border-dashed border-border px-6 py-10 text-center text-sm text-text-muted transition-colors hover:bg-primary-light/15 data-over:bg-primary-light/15"
      >
        <FileUp aria-hidden className="h-6 w-6" />
        <span>Drop the broker&apos;s export here, or choose a file.</span>
        <input
          type="file"
          data-testid="upload-file"
          className="sr-only"
          onChange={onChange}
        />
      </label>
      {error && (
        <Notice tone="error" testId="upload-error">
          {error}
        </Notice>
      )}
    </div>
  );
}

function Preview({ statement }: { statement: Statement }) {
  const rows = statement.rows.slice(0, 5);
  return (
    <table
      data-testid="upload-preview"
      className="w-full table-fixed rounded-md border border-border text-sm"
    >
      <caption className="pb-1.5 text-left text-xs font-semibold tracking-wider text-text-muted uppercase">
        Sample
      </caption>
      <thead>
        <tr>
          <Th dense className="w-28">
            Order date
          </Th>
          <Th dense>Key</Th>
          <Th dense numeric className="w-24">
            Quantity
          </Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => (
          <tr key={i} className="border-t border-border">
            <Td dense className="font-mono tabular-nums">
              {r.orderDate}
            </Td>
            <Td dense className="truncate" title={keyLabel(r.key)}>
              {keyLabel(r.key)}
            </Td>
            <Td dense numeric title={r.quantity}>
              {formatQuantity(r.quantity)}
            </Td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
