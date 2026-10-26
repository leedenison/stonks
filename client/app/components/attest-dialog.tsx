"use client";

import { useEffect, useState } from "react";
import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { IdentifierChip } from "@/app/components/identifier-chip";
import { Input, Select } from "@/app/components/input";
import { Notice } from "@/app/components/notice";
import { Skeleton } from "@/app/components/skeleton";
import type { Identifier } from "@/gen/type/v1/type_pb";
import { useAdminInstruments } from "@/hooks/use-admin-instruments";
import { useAttest, useCoverageTicker } from "@/hooks/use-coverage";
import { instrumentName, intervalAt } from "@/lib/coverage";
import { refusal } from "@/lib/refusal";

// The search box waits this long after the last keystroke before it asks
// for instruments.
const searchDelay = 300;

// AttestDialog lets an administrator assert which instrument a ticker names,
// or that it names nothing, on a day or over a range. It starts from what the
// ticker's validity says on the first day. A refusal stays in the dialog.
export function AttestDialog({
  ticker,
  from,
  onClose,
  onDone,
}: {
  ticker: Identifier;
  // The first day, as YYYY-MM-DD.
  from: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const read = useCoverageTicker(ticker.domain, ticker.value);
  const attest = useAttest();
  const [day, setDay] = useState(from);
  const [to, setTo] = useState("");
  const [namesChoice, setNamesChoice] = useState<
    "instrument" | "nothing" | null
  >(null);
  const [chosen, setChosen] = useState<{
    id: string;
    name?: Identifier;
  } | null>(null);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const found = useAdminInstruments({ after: "", q: query, class: "" });

  // Until the administrator chooses, the dialog follows the ticker's
  // validity on the first day: the instrument it names, or nothing.
  const held = read.data?.ticker
    ? intervalAt(read.data.ticker.intervals, from)
    : undefined;
  const names = namesChoice ?? (read.data && !held ? "nothing" : "instrument");
  const instrument = chosen?.id ?? held?.instrumentId ?? "";
  const picked = chosen
    ? chosen.name
    : read.data?.ticker && held
      ? instrumentName(read.data.ticker, held.instrumentId)
      : undefined;
  useEffect(() => {
    if (search === query) {
      return;
    }
    const t = setTimeout(() => setQuery(search), searchDelay);
    return () => clearTimeout(t);
  });

  const submit = () =>
    attest.mutate(
      {
        ticker,
        names:
          names === "nothing"
            ? { case: "nothing", value: true }
            : { case: "instrumentId", value: instrument },
        from: day,
        to: to || undefined,
      },
      { onSuccess: onDone },
    );
  const ready =
    day !== "" &&
    (names === "nothing" || instrument !== "") &&
    !attest.isPending;

  return (
    <Dialog
      open
      onClose={onClose}
      title="Attest"
      testId="attest-dialog"
      footer={
        <>
          <Button
            variant="secondary"
            onClick={onClose}
            disabled={attest.isPending}
          >
            Cancel
          </Button>
          <Button data-testid="attest-start" disabled={!ready} onClick={submit}>
            Attest
          </Button>
        </>
      }
    >
      <div className="flex items-center gap-2 text-sm">
        <span className="text-text-muted">Ticker</span>
        <IdentifierChip id={ticker} />
      </div>
      <div className="flex flex-wrap gap-4 text-sm">
        <label className="flex flex-col gap-1">
          <span className="text-text-muted">From</span>
          <Input
            type="date"
            data-testid="attest-from"
            value={day}
            onChange={(e) => setDay(e.target.value)}
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-text-muted">To (optional)</span>
          <Input
            type="date"
            data-testid="attest-to"
            value={to}
            onChange={(e) => setTo(e.target.value)}
          />
        </label>
      </div>
      <fieldset className="flex flex-col gap-2 text-sm">
        <legend className="text-text-muted">The ticker names</legend>
        <label className="flex items-center gap-2">
          <input
            type="radio"
            name="names"
            data-testid="attest-names"
            checked={names === "instrument"}
            onChange={() => setNamesChoice("instrument")}
          />
          an instrument
        </label>
        <label className="flex items-center gap-2">
          <input
            type="radio"
            name="names"
            data-testid="attest-nothing"
            checked={names === "nothing"}
            onChange={() => setNamesChoice("nothing")}
          />
          nothing
        </label>
      </fieldset>
      {names === "instrument" && (
        <div className="flex flex-col gap-2 text-sm">
          {read.isPending && <Skeleton lines={1} />}
          {picked && instrument !== "" && (
            <div className="flex items-center gap-2">
              <span className="text-text-muted">Instrument</span>
              <IdentifierChip id={picked} />
            </div>
          )}
          <Input
            type="search"
            data-testid="attest-instrument-search"
            placeholder="Search instruments"
            aria-label="Search instruments"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {query !== "" && (
            <Select
              data-testid="attest-instrument"
              size={5}
              value={instrument}
              onChange={(e) =>
                setChosen({
                  id: e.target.value,
                  name: found.data?.instruments.find(
                    (i) => i.id === e.target.value,
                  )?.name,
                })
              }
            >
              {found.data?.instruments.map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name
                    ? `${i.name.value}${i.name.domain ? ` (${i.name.domain})` : ""}`
                    : i.id}
                </option>
              ))}
            </Select>
          )}
        </div>
      )}
      {attest.isError && (
        <Notice tone="error" testId="attest-error">
          {refusal(attest.error, "The attestation could not be written.")}
        </Notice>
      )}
    </Dialog>
  );
}
