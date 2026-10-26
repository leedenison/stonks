---
title: UI refinement
recorded: 2026-10-26
---

# UI refinement

A pass before production over the surfaces that real use shows could serve their task
better.

## Why

A surface is built to the specification of its milestone. How well it serves the work
shows only after someone has used it on real cases. If someone reworks a surface before
then, the rework guesses at the answer.

This note collects the surfaces that call for a rework. A surface is added when use
suggests that it falls short, even while the better shape is still unclear.

## Sketch

Nothing settled. Each area below waits on real use.

### Attest dialog

The dialog asserts which instrument a ticker names, or that it names nothing, on a day or
over a range. Its fit to the task is unproven. Its shape is settled after it has been used
on real validity gaps and unresolved tickers.

Its instrument search finds only the instruments the system already has. An OpenFIGI
search would let an administrator find an instrument that the system has yet to learn. A
hit is a datasource answer. It creates the instrument with the stable identifiers it
carries, as a user's choice of instrument does in [annotations.md](annotations.md).

## Undecided

What each area becomes.

Do the attest dialog and a user's choice of instrument share one OpenFIGI search? If they
do, the search lands with whichever of the two is built first.
