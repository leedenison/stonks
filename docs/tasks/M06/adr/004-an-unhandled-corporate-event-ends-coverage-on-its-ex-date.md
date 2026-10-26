# An unhandled corporate event ends coverage on its ex-date

A corporate event is handled when its ratio states a change in share count. In lowest terms,
both sides of the ratio are whole. The smaller side is at most 100. A 4 for 1 split, a 3 for
2 split and a 1 for 300 reverse split each pass. This test applies to every datasource.

Providers also state price factors as ratios. Massive types a spin-off as a forward split.
Its ratio is the factor of the fall in the parent's price. Massive states the GE spin-off of
2023 as 1281 for 1000. Many of its stock dividends carry a fractional ratio, such as 25.47
for 24.47.

Massive types each corporate event as a forward split, a reverse split or a stock dividend.
A Massive event is handled when it has one of these types. It must also pass the test. EODHD
sends only a ratio, so the test alone decides its events.

Every other corporate event is unhandled. The fetch stores it with its raw type and ratio and
marks it unhandled. Holdings skip it. The fetch records an `unhandled_corporate_event`
finding, which references the stored row. The corporate event also removes the coverage of
its ticker on its ex-date. A holding across the ex-date shows its raw quantity, marked as
unadjusted.

## Considered options

- Trust Massive's type alone. The GE spin-off would then restate a holding of GE by its
  factor. The holding would carry a mark only when another datasource disagreed.
