---
title: Neutral format and the first marshallers
type: task
dependencies: [003]
---

## Scope

The broker neutral format an upload is expressed in, and the marshallers translating
three brokers' exports into it.

In:

- The proto shape of an upload: the claimed broker, the claimed period as a range of
  order dates, and its rows. The upload covers every account at the broker; see
  [../adr/004-an-upload-claims-every-account-at-its-broker.md](../adr/004-an-upload-claims-every-account-at-its-broker.md).
- A row is one leg: the stated key, order and settlement dates, the "as at" date,
  quantity and currency; see
  [../adr/010-a-row-is-one-leg-and-carries-no-correlation.md](../adr/010-a-row-is-one-leg-and-carries-no-correlation.md).
  A cash leg states a currency identifier in its key.
- A stated split: its key, effective date, quantity change, and a ratio where the export
  states one.
- Marshallers for the IBKR QFX statement, the Schwab transaction history JSON and CSV,
  and the Fidelity activity CSV, in the browser, each unit tested against a fixture
  modelled on a real export with its identifiers replaced; see
  [../adr/003-marshalling-happens-in-the-client.md](../adr/003-marshalling-happens-in-the-client.md)
  and
  [../adr/009-the-first-marshallers-are-ibkr-qfx-schwab-json-and-csv-and-fidelity-csv.md](../adr/009-the-first-marshallers-are-ibkr-qfx-schwab-json-and-csv-and-fidelity-csv.md).
- The claimed period, from the export where it states one and from the first and last
  order dates otherwise.

Out:

- Correlations.
- Any server behaviour. The server accepts the format in
  [006](006-upload-ingestion.md).

## Design

All knowledge of the broker's conventions lives in the marshaller. Only it knows the date
each row is stated "as at", whether one line carries several legs, and which identifiers
the export states, so the format gives it a way to express each of these and the server
interprets nothing broker specific. The broker description is the description text as
the export states it.

Where the export supplies one date, the marshaller populates order date and settlement
date from it. Rows the export marks cancelled or awaiting completion are not emitted.
