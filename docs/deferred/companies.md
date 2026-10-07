---
title: Companies
recorded: 2026-10-07
---

# Companies

A company is the issuer of one or more instruments, identified by a CIK or an LEI.

## Why

Massive and EODHD return a CIK, and EODHD an LEI. Each names an issuer rather than an
instrument: one CIK spans every share class a company issues, so neither fits the
instrument grain or the listing grain of an identifier. The system discards them.

## Model

A company is a system owned record carrying its CIK and LEI, and an instrument references
the company that issued it. A datasource answer that carries a CIK or an LEI creates or
confirms the company as it creates the instrument. Two instruments with one issuer are
then related, which groups a company's share classes and depositary receipts and gives
corporate events and filings a subject.

## Open questions

- Whether one datasource's answer writes a company, or a company needs corroboration as
  an instrument does.
- How an LEI that names a fund or a trust rather than the issuer of a share is modelled.
