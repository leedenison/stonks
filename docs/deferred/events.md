---
title: Events
recorded: 2026-09-14
---

# Events

The grouping of transactions into the economic events they are legs of, and the
correlating evidence a source supplies to make that grouping possible.

## Why

A purchase is one economic event and several transactions: the stock acquired, the cash
paid, the commission, the fees and the tax.  Brokers state these in their own way, often
on one line, and do not reliably mint a reference that ties the legs together.  Reporting
cost, income and residual per event needs the legs grouped, and grouping by the broker's
own conventions would tie the server to each source.

## Model

### Events and Legs

An event is derived from its legs as a separate step after ingestion.  Legs are grouped
by the server according to general rules that apply identically to all sources, so an
event can split across statements.

The remainder of an event whose legs do not balance is its residual.

### Correlations

A correlation is evidence a marshaller attaches to a transaction from the source's own
conventions, expressed in a form the server can compare without knowing the source.  A
statement that carries a purchase, its cash debit, its fees and its commission on one
line becomes several transactions sharing one reference.

| Type     | Compares                                       | Direction | Concludes                                          |
| -------- | ---------------------------------------------- | --------- | -------------------------------------------------- |
| EXACT    | equality of the reference                      | symmetric | the transactions are legs of one event             |
| ORDINAL  | distance between two ordinals, within the span | symmetric | the transactions are candidates for one event      |
| ACCOUNT  | the reference against another account          | directed  | the two are candidates for the sides of a transfer |
| ATTACHES | the reference against another transaction      | directed  | the bearer joins the event that transaction is in  |

One reference carries as many of these as are true of it; a reference that numbers
sequentially supplies both equality and proximity.  A directed correlation is carried by
one side only, and the transaction it names says nothing in return.

## Sketch

Grouping runs over a neighbourhood of what was uploaded rather than over one statement or
over everything.  The neighbourhood reaches as far as the evidence does, which is not a
window of dates: a correlation a user asserted can link two transactions years apart.

Grouping is a run.  See [runs.md](runs.md).

The neutral format gains correlations when grouping is built.  Statements made before then
carry none and are uploaded again to gain them.

## Undecided

- The general rules that group legs carrying no correlation: whether date, instrument and
  balancing amounts are enough, and what a leg matching nothing becomes.

- How a residual is shown, and whether an event with a residual is complete.

- Whether a user can assert or break a grouping, and whether such an assertion is a
  correlation of its own.

- When grouping reruns: on every statement touching the neighbourhood, on a replay that moves
  a transaction, or on demand.
