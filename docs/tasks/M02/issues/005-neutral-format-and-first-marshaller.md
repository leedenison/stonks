---
title: Neutral format and the first marshaller
type: task
dependencies: [003]
---

## Scope

The broker neutral format an upload is expressed in, and the marshaller translating the
first broker's export into it.

In:

- The proto shape of an upload: the claimed broker, the claimed period and its rows.
  The upload covers every account at the broker; see
  [../adr/004-an-upload-claims-every-account-at-its-broker.md](../adr/004-an-upload-claims-every-account-at-its-broker.md).
- A row: the stated key, order and settlement dates, the "as at" date, quantity and
  currency, and a stated split where the export states one. A cash leg states a
  currency identifier in its key.
- The marshaller for the first broker's export, in the browser, unit tested against a
  fixture modelled on a real export with its identifiers replaced; see
  [../adr/003-marshalling-happens-in-the-client.md](../adr/003-marshalling-happens-in-the-client.md).

Out:

- A second broker or channel.
- Any server behaviour. The server accepts the format in
  [006](006-upload-ingestion.md).

## Design

All knowledge of the broker's conventions lives in the marshaller. Only it knows the date
each row is stated "as at", whether one line carries several legs, and which identifiers
the export states, so the format gives it a way to express each of these and the server
interprets nothing broker specific.

Where the export supplies one date, the marshaller populates order date and settlement
date from it.
