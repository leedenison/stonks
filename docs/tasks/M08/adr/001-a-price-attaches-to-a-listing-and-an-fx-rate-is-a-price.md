# A price attaches to a listing, and an FX rate is a price

Every price references one listing. A listing is one currency family of an instrument,
and each datasource client states the currency of a price. See
[004](004-a-price-is-stored-in-the-family-currency-of-its-listing.md). So a price always
knows its listing.

An FX rate is a price too. Each currency family is an instrument of class cash. The rate
of a currency is the price of the USD listing of that instrument: the number of USD that
one unit of the currency buys. A conversion between two other currencies goes through
USD, which keeps the stored pairs linear in the number of currencies.

One table then holds prices and rates. They share one coverage record, one precedence
rule and one fetch path. A rate is stated as at its own date and a currency never splits,
so the restatement of a price leaves a rate as it is.

## Consequences

* A client inverts a rate where its source quotes the currency per USD.
* The USD listing of a currency is created when the currency comes into use. The seed
  creates only the listing of each currency in its own family.
* The details page of a currency is the instrument details page. Its rate chart is the
  price chart.
* Venues are fungible within a listing, and their closes differ by a spread at most. A
  fetch asks at the primary venue where the listing states one. Otherwise it asks at any
  venue that has an open ticker. The fetch key records the ticker sent.
