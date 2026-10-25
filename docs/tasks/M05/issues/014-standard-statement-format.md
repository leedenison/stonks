---
title: Standard statement format
type: task
---

## Scope

A statement in the standard format is one JSON document that holds a
`stonks.statement.v1.Statement` message in its protojson encoding. The upload dialog
takes it beside the broker exports. A user converts the export of an unsupported broker
to it outside the client. A test fixture in it states whatever the RPC takes.

In:

- The document is the protojson encoding of `Statement`, as `proto/statement/v1/statement.proto`
  defines it. The proto file is the definition of the format. When a field is added to
  the message, it becomes part of the format.
- A marshaller for the format. It recognises a file whose media type is JSON and whose
  text parses as a `Statement`. When a document fails to parse, the dialog refuses it
  with the parser's message, as it does for a broker export.
- The document states its broker and its period. The dialog shows the broker the
  document states. The user may narrow the period as for any export.
- The format omits an export date. Every quantity is as at its row's `as_at`.
- The end to end specs that build a statement through the API upload a fixture in the
  format instead. Each dated key the suite uses then arrives through the dialog.
- The marshal package comment names the format. A sample document sits in the
  marshaller's test data.

Out:

- Downloading a statement in the format.
- Converters from broker exports to the format outside the client.
- A venue on the tickers the broker marshallers state.

## Motivation

The three marshallers state what their exports state. Two state a bare ticker and one
states a CUSIP beside its ticker, so every upload through the dialog leaves its keys
fixed. The coverage page, the extend validity run and the transaction marks all read
dated keys, and the acceptance pass cannot reach them through the UI. The specs that
cover them build each statement through the API.

A format the client defines gives a user with an unsupported broker a target. It also
gives the suite a fixture that states a venue ticker without a stable identifier.

## Design

The format is the message rather than a schema of its own. One definition serves the
RPC, the marshaller and the document. The generated codec parses it on the client, and
protovalidate checks it on the server as it checks every statement. A document holds
only the message. The format is unversioned, and its shape changes in place with the
proto.

The protojson conventions apply:

- A field is named as protobuf-es writes it, in lower camel case: `orderFrom`. The
  proto name `order_from` is accepted too.
- An enum value is written by its full name: `BROKER_IBKR`, `ASSET_CLASS_STOCK`,
  `IDENTIFIER_TYPE_MIC_TICKER`.
- A date and a decimal are the strings the message holds.
- A reader leaves an absent field absent.

A document that states one venue ticker:

```json
{
  "broker": "BROKER_IBKR",
  "orderFrom": "2024-01-01",
  "orderBefore": "2025-01-01",
  "rows": [
    {
      "key": {
        "identifiers": [
          {"type": "IDENTIFIER_TYPE_MIC_TICKER", "domain": "XNAS", "value": "META"}
        ],
        "assetClass": "ASSET_CLASS_STOCK",
        "currency": "USD"
      },
      "orderDate": "2024-03-01",
      "settlementDate": "2024-03-04",
      "asAt": "2024-03-01",
      "quantity": "10"
    }
  ]
}
```

The marshaller list is keyed by format. A broker export is one format whose broker is
fixed.
