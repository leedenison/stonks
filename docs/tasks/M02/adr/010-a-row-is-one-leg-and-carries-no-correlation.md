# A row is one leg and carries no correlation

A row of the neutral format is one leg: one stated key and one quantity. An export line
carrying a trade's security, cash, commission and tax becomes one row per leg, and the
marshaller emits them in that form.

Nothing links the rows of one line. Correlations, the evidence the server groups legs by,
are left out of the format until grouping is built. Holdings are derived from quantities
alone and do not need a fee leg told apart from a consideration leg.

## Consequences

Uploads made before correlations exist carry none, and are re-uploaded to gain them.
Replacement of the claimed period makes that cheap.
