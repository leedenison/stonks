# A fetch covers the days held, and a day without a session takes the close before it

## Prices

The demand for the prices of a listing is the union, across users, of the days on which
each holds the instrument. A gap is a demanded day that every datasource leaves
uncovered. Only a priced instrument is demanded. Every class is priced except cash, an
option and a future. An unresolved key is left out in this milestone.

A holding is of the instrument, and its listings are summed. A holding demands the
listing that its key names. The holdings page places the holding under the same listing:

* A key with a fixed association names the listing that the association stores.
* A key dated through a ticker names the listing in the family of its stated currency.
  Where the key states none, it names the listing that its ticker reaches at its venue.
* Any other key names only the instrument, and the holding demands every listing of it.

An open holding runs to the last completed day, which is the UTC day before today. See
[009](009-a-ticker-answer-is-dated-by-its-utc-day.md).

## The days held

A user holds an instrument while the quantities of that user's transactions on it sum to
a value other than zero. Each quantity is restated to today through the corporate actions
calendar first. A sum of the stated quantities would close a holding that was bought
before a split and half sold after it.

Where the calendar does not cover a transaction, the sum cannot be trusted. The holding
then counts as held from its first day to the last completed day.

## Days without a session

A weekend or a holiday is covered and lacks a price. A read values such a day at the
latest earlier close, provided the coverage between the two days is unbroken. A break in
coverage reads as unknown. The coverage is that of every datasource together, and the
close is the latest earlier one whatever the rank of its datasource.

A holding can begin on such a day, as a transfer in on a Saturday does. The demand
therefore starts one week before the first day held, so an earlier close exists.

## Rates

A currency is in use when it is the price currency of a held listing, the currency of a
cash holding, or a display currency a user selected. Each currency in use is fetched
against USD from one week before the first day on which any user holds anything to the
last completed day. The week gives a read an earlier close to carry across a weekend,
because a rate datasource may omit the weekend days.

Tracking the days of each currency for each user was the alternative. It needs the
display currency of each user and the days of each cash holding. A rate series is one key
per currency and there are few currencies, so one span for every currency costs little.
