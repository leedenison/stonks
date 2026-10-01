---
title: Holdings of a family scale to its unit
type: task
---

## Scope

A cash holding summed across the codes of one currency family, so a key stating pence
and a key stating pounds on one instrument sum in pounds.

## Design

Nothing settled. A family is a unit prefix today: the currencies table names each code's
family and nothing records the scale between them. Holdings sum quantities per instrument
without scaling, so the scale belongs either on the currencies row, applied where holdings
are summed, or on the transaction as it is written.
