# A price is stored as its source served it and restated on read

## What is stored

Each price is stored with the "as at" date that its datasource client states. Only the
client knows the conventions of its source.

* A client that can ask for as traded prices does, and states each price as at its own
  date. Massive and EODHD both allow it.
* Google Sheets serves only closes adjusted for splits. Its prices are stated as at the
  day the spreadsheet evaluated them. See
  [007](007-the-google-sheets-tool-writes-formulas-and-reads-pasted-values.md).
* Every client stores a price free of any dividend adjustment. The calendar restates for
  share counts alone, so the adjustment could not be undone, and it would leave a step
  where two sources meet in one series.

An as traded price is preferred because a back adjusted one loses precision. A close of
143.00 before a 20 for 1 split is served as 7.15, and a source that rounds to two places
is off by a visible fraction once the price is multiplied back.

## What a read does

A read restates each price from its "as at" day to today, through the corporate actions
calendar. The factor is the inverse of the one that restates a quantity over the same
days. An action therefore restates a price exactly when it restates a holding, including
an action that an administrator decided.

When the calendar does not cover every day from the "as at" day to today, the price is
provisional, as a holding is. It follows every action known so far.

## Considered options

Storing the restated value beside the price was considered and left out.

* The value would be recalculated on each of: a refetched corporate actions answer, an
  administrator's decision, a rebuild of ticker validity, a merge of two instruments, and
  a change to the corporate actions source of a market. A missed trigger leaves a value
  wrong by a split ratio, and the system cannot detect it.
* The read is cheap. The calendar costs a fixed number of queries however many
  instruments are asked. The factor changes only at each ex date, so restating a series
  is one multiplication per point. The price rows are read either way.
* Issue [010](../issues/010-price-chart.md) carries a benchmark that holds the read to a
  bound. If the read fails the bound, the value is stored.

## Consequences

* A Google Sheets price needs the calendar only from the day of its evaluation to today,
  because Google applied every earlier split. An as traded price needs the calendar from
  its own date to today.
* The provisional mark covers a corporate actions source that has not answered. When a
  source answers and misses a split, the price is wrong by the ratio and reads as sound.
