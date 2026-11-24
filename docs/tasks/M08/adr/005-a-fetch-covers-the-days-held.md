# A fetch covers the days held, and a day without a session takes the close before it

## Prices

The demand for the prices of a listing is the union, across users, of the days on which
each holds the instrument. A gap is a demanded day that every datasource leaves
uncovered. Only a priced instrument is demanded. An option and an unresolved key are left
out in this milestone.

A holding is of the instrument, and its listings are summed. A holding demands the
listing that its key names through a ticker. Where the key names only the instrument, as
an ISIN does, the holding demands every listing of the instrument.

An open holding runs to the last completed day. That day is read in the time zone of the
market of the listing, so a Tokyo listing does not wait for a UTC midnight. See
[009](009-a-ticker-answer-is-dated-by-the-local-date-of-its-market.md).

## Days without a session

A weekend or a holiday is covered and lacks a price. A read values such a day at the
latest earlier close, provided the coverage between the two days is unbroken. A break in
coverage reads as unknown.

A holding can begin on such a day, as a transfer in on a Saturday does. The demand
therefore starts one week before the first day held, so an earlier close exists.

## Rates

A currency is in use when it is the price currency of a held listing, the currency of a
cash holding, or a display currency a user selected. Each currency in use is fetched
against USD from the first day on which any user holds anything to the last completed
day.

Tracking the days of each currency for each user was the alternative. It needs the
display currency of each user and the days of each cash holding. A rate series is one key
per currency and there are few currencies, so one span for every currency costs little.
