# A price is stored in the family currency of its listing

A listing is keyed by its currency family, so a London listing says GBP whether a source
quotes it in pounds or in pence. The unit is a property of each source, and two sources
need not agree.

The system stores every price in the family currency of its listing. A price row lacks a
currency column, because the listing states the family. Each datasource client states the
code its source quoted, because only the client knows the conventions of its source. The
server checks that the code belongs to the family of the listing, multiplies the price by
the `scale` of the code, and stores the result. The scale is a decimal and the column is
numeric, so the conversion is exact.

Storing the quoted code on each row was the alternative. Each row would then be correct,
but rows of one listing could mix units, and every read and every comparison between
sources would apply the scale first.

A cash holding applies the scale on read, because its stated key keeps the code the
user's statement gave. A price is system data. The code its source quoted need not be
kept, so the price converts on write.

## Consequences

* The server refuses a code outside the family of the listing, and a code the system
  lacks. The day stays a gap.
* An FX rate is between two family currencies, so the scale applies once, on the price.
* A client that reports the wrong code stores a price wrong by the scale. The family
  check cannot detect it.
* The Google Sheets tool sends the code that a `currency` cell beside each series states.
  The EODHD client takes the code from the metadata of the symbol, since its daily bars
  omit the currency. Massive serves US listings alone, so its client states USD.
