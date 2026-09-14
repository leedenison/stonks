# The first marshallers are IBKR QFX, Schwab JSON and CSV, and Fidelity CSV

Three brokers come first so that the neutral format is shaped by harmonising across
brokers rather than by one export.

- IBKR: the QFX investment statement, OFX 1.02 SGML. A Flex Query is configured by the
  user, so no two Flex Query CSVs are one format.
- Schwab: the transaction history JSON and CSV, which carry the same rows. The JSON
  states the from and to dates; the CSV states no period.
- Fidelity (UK): the activity CSV. Its preamble carries the timeframe and one export
  covers every account.

The claimed period is prefilled from the export where it states one, and from the first
and last order dates otherwise. The user can change it before the upload starts.

What the three state, and what the format expresses as a result:

- **Dates.** Fidelity states an order date and a completion date. IBKR states a trade
  timestamp with its zone and no settlement. Schwab states one date, and on some rows a
  posted date "as of" an effective date, which is the order date with the posted date as
  settlement. Settlement before order is stated by real exports and is not an error.
- **As at.** Each states a split as a line of its own and leaves earlier quantities as
  they were, so every row is stated as at its order date. The row carries the date all
  the same, since only the marshaller knows this.
- **Legs.** Schwab and IBKR carry a trade's security, cash, commission and tax on one
  line. Fidelity states each on its own line. See
  [010](010-a-row-is-one-leg-and-carries-no-correlation.md).
- **Splits.** Schwab states the quantity added on an effective date and no ratio. IBKR
  states the quantity added, and the ratio only in free text. A stated split is a key, an
  effective date, a quantity change, and a ratio where the export states one.
- **Currency.** IBKR states a currency per row. Schwab and Fidelity state none, and the
  marshaller states USD and GBP.
- **Broker description.** The description text is matched exactly. It drifts between
  exports of one broker for one instrument, so several descriptions from one broker come
  to name one instrument, each a listing grain identifier in the domain of its broker and
  channel. Until a datasource answers for the identifiers in their stated keys, each
  resolves to an instrument of its own.
- **Other identifiers.** IBKR states a CUSIP or ISIN for a stock and a contract id for an
  option. Schwab and Fidelity state a symbol without a venue, which is a search hint and
  not an identifier. All are carried in the stated key; only the broker description is
  admitted in this milestone.
- **Asset class.** IBKR distinguishes stock trades from option trades. Schwab and
  Fidelity distinguish nothing beyond cash. Each marshaller states the narrowest class it
  can defend: equity or option for IBKR, security for the others, cash for a cash leg.
- **Rows not emitted.** Fidelity marks cancelled rows and rows awaiting completion.
  Neither is emitted; replacement means a later export supplies the completed row.
