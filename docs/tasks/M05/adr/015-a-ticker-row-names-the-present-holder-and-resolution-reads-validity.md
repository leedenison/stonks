# A ticker row names the present holder, and resolution reads validity

A `MIC_TICKER` row in `identifiers` is a name. It sits on the listing of the instrument
that the value's latest assertion or event names, and `UNIQUE (type, domain, value)`
stays, so one instrument shows a ticker at a time and a reassigned value shows on its new
holder. What a value names on a given day is read from its validity.

Four rules follow:

- Resolution never looks a MIC-derived value up through its row. A key stating a venue
  ticker reads the value's validity. Where the value has facts, the key takes a dated
  association at once, and no fetch runs for dates the facts already confirm. Where it
  has none, the identity fetch runs, and its answer writes the facts.
- Only a present-dated answer writes or moves the row. An identity answer, or a lookup
  on the current day, that names a holder other than the row's moves the row to the new
  holder's listing. A past-dated answer that names an instrument the system lacks
  creates it with its stable identifiers and writes its assertion, without a ticker row.
- A retirement leaves the row where it is, so a delisted holding keeps its name.
- `validity.Record` maps an assertion to an instrument through stable identifiers only.
  The ticker row names the present holder, which is the wrong instrument for a
  past-dated answer.

## Considered options

Dropping uniqueness for MIC-derived types, so every past and present holder carries the
value. A historical ticker would then read as a current name, and `instrument_names`
would have to consult validity to choose one, which puts the derivation into a SQL view.

## Consequences

A holding whose transactions mostly predate a rename is still named by the current
ticker. That is what naming a holding by its ticker means.
