# An unhandled corporate action ends coverage on its ex-date

A corporate action is handled when its ratio states a change in share count. In lowest terms,
both sides of the ratio are whole. The smaller side is at most 100. A 4 for 1 split, a 3 for
2 split and a 1 for 300 reverse split each pass. This test applies to every datasource.

Providers also state price factors as ratios. Massive types a spin-off as a forward split.
Its ratio is the factor of the fall in the parent's price. Massive states the GE spin-off of
2023 as 1281 for 1000. Many of its stock dividends carry a fractional ratio, such as 25.47
for 24.47.

Massive types each corporate action as a forward split, a reverse split or a stock dividend.
A Massive corporate action is handled when it has one of these types. It must also pass the test. EODHD
sends only a ratio, so the test alone decides its corporate actions.

A price factor with a small enough ratio passes the test and is handled. EODHD states the GE
spin-off of 2019 as 104 for 100. In lowest terms that is 26 for 25, which reads as a split.
This is accepted. When another datasource covers the ticker and lacks the corporate action, the fetch
raises a `corp_act_disagreement` finding.

Every other corporate action is unhandled. The fetch stores it with its raw type and ratio and
marks it unhandled. Holdings skip it. The fetch records an `unhandled_corp_act`
finding, which references the stored row. The corporate action also removes the coverage of
its ticker on its ex-date. A holding across the ex-date shows its raw quantity, marked as
unadjusted.

## Considered options

- Trust Massive's type alone. The GE spin-off would then restate a holding of GE by its
  factor. The holding would carry a mark only when another datasource disagreed.
