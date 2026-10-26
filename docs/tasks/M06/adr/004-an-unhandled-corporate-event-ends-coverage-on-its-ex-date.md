# An unhandled corporate event ends coverage on its ex-date

Massive types each corporate event as a forward split, a reverse split or a stock
dividend. All three restate a quantity by their ratio. EODHD sends only a ratio, so every
corporate event it sends applies.

When Massive sends any other type, the fetch stores the corporate event with its raw type
and ratio and marks it unhandled. Holdings never apply it. The fetch records an
`unhandled_corporate_event` finding that references the stored row. The corporate event
also removes the coverage of its ticker on its ex-date. A holding across it therefore
shows its marked raw quantity.
