# Price coverage ends at the last price served

A covered day without a price had no session, and the first answer of a datasource for a
day stands. See
[002](002-datasource-precedence-picks-the-price-for-each-day.md). A client that covers
the whole period it was asked for can therefore store a wrong day for good. Two cases do
it:

* A provider publishes a close some hours after the session ends. The last completed day
  is a UTC day, so a fetch can run before the close of that day is published. See
  [009](009-a-ticker-answer-is-dated-by-its-utc-day.md).
* A provider may answer an unknown ticker as a known ticker that has not traded. Massive
  answers both with an empty list.

So the coverage of an answer ends on the day of the last price that the provider holds
for the identifier, whatever period was asked. The coverage of an answer without a
price is empty. Every price client follows the rule.

## Where coverage starts

Coverage starts at the window of the plan. EODHD does not state the day on which its
history of a symbol starts, and that day can fall after the listing began to trade. So
the EODHD client starts coverage at the first price served when that day is later than
the window. The days before it stay gaps at EODHD. A run asks EODHD again for each of
those days on which a user holds the instrument, and the Google Sheets tool sees them.

## Considered options

Covering the days after the last price was the alternative. It closes a weekend gap
sooner, and it stores an unpublished close as a day without a session.

## Consequences

* An unknown ticker stays a gap and passes down the precedence.
* A weekend or a holiday inside the history of a provider is covered and lacks a price.
* The days after the last session stay gaps until the next session is served. On a Sunday
  or a Monday the weekend days of every held listing are gaps. A run asks each datasource
  for them in turn, and the Google Sheets tool sees them.
* After a delisting, the later days stay gaps at that datasource.
