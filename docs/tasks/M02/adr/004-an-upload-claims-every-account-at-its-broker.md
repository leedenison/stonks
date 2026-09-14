# An upload claims every account at its broker

A user may hold several accounts at one broker, and a broker's export may cover any of
them. An upload is taken to contain every account the user holds at the claimed broker
over the claimed period.

Replacement is therefore keyed on user, broker and period, not on account. An upload that
omits an account deletes that account's transactions in the period, which is the same
rule that makes an empty range at a boundary a deletion.
