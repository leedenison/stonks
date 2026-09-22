---
title: Annotations
recorded: 2026-09-21
---

# Annotations

What a user records against their own stated keys: a pin to an instrument, a grouping, a
price, and a key made by hand for an asset no source states.

## Why

An unresolved key is still a holding, and the user knows things about it that
no source can state: which instrument it is, that two keys are one holding, what it is
worth. Some assets, a house or a mortgage, are stated by no source at all. In a
multi-user system that knowledge reaches the user's own holdings only, so it is recorded
against keys, which are the user's, and never against instruments, which are shared.

## Model

Every annotation is keyed on the user and an identifier triple or a key, never on an
instrument, so it survives the replacement of keys a re-upload performs. No annotation
writes system owned data: a pin creates no identifier row and is not an assertion.

- A pin associates a key with an instrument, standing in for resolution's answer for that
  user's key alone.
- An identifier a user adds to a key is what joins two holdings no source stated in
  common. Grouping already gathers the keys sharing an identifier, so this needs no
  grouping of its own and is honoured every time groups are recomputed. There is no
  exclusion: a key that wrongly joins two holdings is a defect in the export, and the
  fix is to correct the export.
- A price is a value on a date for one identifier triple, in the currency the key states.
  A holding's user price series is the union over its identifiers' series, the latest
  entry winning on a date.
- A manual key is created by hand through a channel of its own, with transactions
  entered against it, for assets and liabilities no source states. Their asset classes
  sit at the root of the tree and no integration serves them, so they are never sent
  anywhere.

## Sketch

Valuation splits by class. Cash and liability value at quantity, so a mortgage is a
liability holding in a currency valued at its balance, with interest entered as
transactions. Everything else values at quantity times price, from the instrument's
prices where the key resolved and from the user's series otherwise.

## Undecided

- Whether system prices take precedence over a user's series for a resolved key, or fill
  only the dates the user left blank.
- The currency of a group whose keys state codes of one family, GBP and GBX.
- Whether a pin informs resolution's ranking of candidates for other users' keys, or
  stays entirely private.
- Whether interest on a liability is generated from a rule or always entered.
