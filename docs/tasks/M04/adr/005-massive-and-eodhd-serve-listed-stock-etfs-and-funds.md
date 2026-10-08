# Massive and EODHD serve listed stock, ETFs and funds

Massive and EODHD each answer for more than the system resolves. The integrations serve
three classes, stock, etf and mutual_fund, on listed markets. OTC markets are excluded.
EODHD's coverage is worldwide, across the 70 exchanges its exchanges list names. Massive's
is the US exchanges.

An integration declines a key before asking when the key places it outside the
provider's coverage. It declines a key that states a class other than stock, etf or
mutual_fund, or a parent of them. It declines a ticker at a venue outside the provider's
venue table. Every OTC venue is outside the table. When the integration declines a key, the
provider records no coverage of it. When a key states only a stable identifier that the
provider accepts, or a ticker without a venue, the integration asks the provider.

Resolution does not check an answer against the provider's coverage. Each integration's
conversion produces candidates only for the classes and markets it serves:

- A record converts to a candidate only when its type maps to one of the three classes.
  Warrants, rights, units, notes, bonds and structured products do not convert.
- Massive states OTC tickers under the `otc` market, so a Massive record converts only
  when its market is `stocks`.
- EODHD's code US spans the OTC venues as well as the exchanges. A US row converts only
  when the exchange symbol list names an exchange, through the venue table of
  [003](003-each-provider-maps-its-venues-through-a-generated-table.md).
