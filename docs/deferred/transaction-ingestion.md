---
title: Transaction ingestion
recorded: 2026-09-05
---

# Transaction ingestion

Takes a batch of transactions as some source states them (eg. a broker statement, a trade
confirmation email, a user exported archive, etc) and records them as that user's
transaction log.  Invokes fetches of corporate events, resolves instruments and fetches
prices to maintain the relevant invariants w.r.t. to the period that instruments are
held.

## Why

Importing unmodified broker statements or trade confirmation emails is the minimal
friction path for the user.

## Model

### Transactions and Events

Transactions have no natural key and are created with an internally scoped primary key.
It is possible to have two transactions with identical settlement date, user, broker,
description, instrument and listing.  Brokers do not reliably include any reference that
they mint for a transaction in their statements. Many brokers also include several legs
of an event in a single line in their statements. So legs cannot be distinguished by a
broker reference.  

Transactions carry both order date and settlement date.  These are usually supplied by
brokers but where only one date is provided the marshaller should populate both from the
same date.

An event is derived from its legs as a separate step after ingestion. Legs are grouped by
the server according to general rules that apply identically to all sources.  Transactions
must therefore carry a general form of evidence correlating the legs of an event derived
from the broker specific conventions by the client.  This allows events to split across
uploads.

Grouping transactions into events is outside the scope of transaction ingestion but
providing mechanisms to supply correlating evidence is in scope.

### Sources, Channels and Uploads

The source and channel combination determines the format, which information is available,
and the authority the data carries.

Since transactions have no natural key a user's upload and its claimed broker and coverage
period are the unit of idempotency.  An upload replaces all transactions within the
claimed period for the claimed broker.  It states the period it covers rather than leaving
that to be inferred from the transactions inside it, so that empty ranges at the
boundaries act as deletions.

An upload is user authenticated, so the data it carries is limited to user authority
unless corroborated and upgraded by a system authoritative source.

### Correlations

Correlations allow the broker specific marshaller to provide additional evidence to aid
grouping of transactions into economic events.  For example, broker statements will
commonly include an acquisition of stock, the cash debit, fees and commission on a
single line item.  The system expects these to be broken into distinct line items but
we want to preserve the fact that they belong to a single economic event.  Correlations
allow the broker specific marshaller to annotate each leg with an equality reference
that the server can use to group them.

The proposed correlation types are:

| Type     | Compares                                       | Direction | Concludes                                          |
| -------- | ---------------------------------------------- | --------- | -------------------------------------------------- |
| EXACT    | equality of the reference                      | symmetric | the transactions are legs of one event             |
| ORDINAL  | distance between two ordinals, within the span | symmetric | the transactions are candidates for one event      |
| ACCOUNT  | the reference against another account          | directed  | the two are candidates for the sides of a transfer |
| ATTACHES | the reference against another transaction      | directed  | the bearer joins the event that transaction is in  |

One reference can carry as many of these as are true of it; ie. a refernce that numbers
sequentially supplies both equality and proximity.  A directed correlation is carried by
one side only, and the transaction it names says nothing in return.

## Constraints

### As At Convention

User uploaded transactions must initialise the "as at" date for each transaction which
declares the date at which the values were true.  This allows the server to know which
corporate events a transactions price and quantity are adjusted for.  For example, a
broker that conventionally restates transactions as corporate events arise might
export a transaction statement before a particular corporate event is known.  If that
statement is subsequently uploaded after the corporate event ex. date the server
cannot know if it has been restated without the "as at" date.

A typical convention for most brokers is that a transaction is stated as at the order
date of the transaction.  However, only broker specific marshallers can know the
conventions of a particular broker. So the marshaller must interpret the conventions
and provide an explicit "as at" date to the server.

### The Source Boundary is a Neutral Format

Marshallers translate from broker specific formats to the neutral format of the system.
All knowledge of broker specific conventions, assumptions or implications must be
contained within the marshaller.  The neutral format must provide ways for those
conventions, assumptions and implications to be expressed in a broker neutral format.

### Ingestion Error Handling

The promise of idempotence when re-uploading a period of transactions for the same source
is intended to make error handling cheap for the system and the user during ingestion.
When a row is invalid, it can be rejected without rejecting the entire file.  The user can
then decide to correct the error and re-upload it cheaply.  Or they can choose to ignore
the error if, for example, they believe the error is uncorrectable and they want to
tolerate the discrepancy without losing the remaining transactions in the file.

The UI must be very clear when transactions have been rejected during ingestion both at
the time of the upload and when a user browses previous uploads.

### Ingestion is Background Work

An upload takes long enough that the request starting it cannot wait for it.  The call
starting an upload answers with the identity of the work rather than with its result.
Progress, the outcome and the rows that failed are read back against that identity.

# Established Stonks Datamodel Conventions

The following conventions are established and documented in the
003-transactions.sql migration file level comments:

Database transactions should use dbTx to distinguish from financial transactions.

## Invariants

### Resolution Attempted for All Transactions

Instrument resolution, corporate event fetching and price fetching are attempted at
ingestion time for any new instruments or time periods covered.    

## Sketch

Ingestion runs in stages.  The client marshals its source into the neutral format and
uploads it; the service validates what arrived, resolves the instruments, fetches
relevant corporate events, fetches relevant prices, fetches relevant FX rates,
writes the transactions, and partitions them into events.

Resolution is keyed on what the source stated, so one description is resolved once for the
whole upload however many transactions carry it.

Grouping runs over a neighbourhood of what was uploaded rather than over one upload or over
everything.  The neighbourhood reaches as far as the evidence does, which is not a window
of dates: a correlation a user asserted can link two transactions years apart.

## Undecided
