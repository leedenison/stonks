---
title: Guesses
recorded: 2026-10-07
---

# Guesses

Where the stated data leaves an association open, a guess ranks the candidates, and the
choice it makes is recorded against the key for the user to confirm or revise.

## Why

A key often states less than resolution needs.  A bare ticker names a listing on more
than one venue.  An ISIN names an instrument but not the listing the broker traded.  A
datasource cannot answer for a broker description.  Without a guess such a key stays
unresolved until the user supplies what is missing, and the holdings it would produce are
absent until then.

## Model

### Sources

A guess source has candidate authority.  Two are foreseen:

- A language model reads what the key states, a description or a bare ticker, and
  proposes the identifier it most likely names: a MIC_TICKER with its venue, or a stable
  identifier.  The proposal is a lookup for a datasource to answer, and the answer is the
  candidate.
- Broker configuration states the currencies a broker lets its customers trade.
  Fidelity.co.uk, for example, trades USD, GBP/GBX and EUR only.  Where an instrument
  lookup hits and the key does not state a currency, the configuration ranks the
  instrument's listings.

A user may also state a global currency ranking.  It ranks listings where the broker
configuration is silent.

### Arbiter

An association records its validity and its arbiter separately.  The arbiter is stated,
datasource, guess or user.  See [terminology.md](terminology.md).

### Authority

A guess does not write instrument data.  The instrument, listing and identifiers a
candidate names come from the datasource that answered, with system authority, whether
the key's own identifiers were the lookup or a guess proposed it.  A guess's rank may
decide an association.

### Replay

A guessed association is replayed as an unresolved key is: when a datasource gains
coverage, an integration is enabled or a guess source changes.  An association the user
arbitrated is never replayed into a different candidate.

## Sketch

Resolution consults the stated data, the database, the batch cache and the datasources
first.  A guess ranks at two grains.  It ranks instruments where the lookups miss and
leave the instrument open.  It ranks listings where the instrument is found and the
stated data leaves the listing open.  Where the key leaves the currency open, a datasource
with complete coverage settles the listing, and one with limited coverage returns
candidates for the listing guess.

Each identity integration declares whether its coverage is complete.  It declines a key
that states a venue or a class outside its coverage, without asking the provider.  Its
conversion produces candidates only for the venues and classes it serves, and resolution
accepts each candidate as given.

A candidate that agrees with a guess ranks below one that confirms stated data or
corroborates a higher precedence answer.  It ranks above the datasource's own order.

Resolution always takes the top rank and records the arbiter as guess.  There is one
review surface, in the upload flow and later: it lists the user's guessed associations,
each with the candidates the guess ranked, and the user confirms the top rank or picks
another.  Either writes the arbiter as user.

A language model source is an integration like any other.  Its traffic is recorded and
redacted as the recording proxy records every provider.

## Undecided

- Where the candidates a guess ranked are stored, so the review surface can show them.
  fetch_identifiers stores one set of identifiers per fetch key.

- Whether a replay that changes the top rank replaces a guessed association silently, or
  holds the change for the user once they have seen the guess.

- What an association the user arbitrated does when an identifier event ends the
  validity of the identifier that carries it.

- Whether broker configuration is shipped as reference data or set by an
  administrator.

- How a guess that proposes nothing is recorded, so the same key is not sent to the
  model on every replay.
