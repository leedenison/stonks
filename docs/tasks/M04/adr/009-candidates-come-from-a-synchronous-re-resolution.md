# Candidates come from a synchronous re-resolution

The user confirms one candidate for a key without an association. A served fetch
records only the identifiers of the group that attached, so the candidates of an
unrecognised key are lost. Each time the user asks, resolution runs the key again and
lists what the datasources answer, through the fetch cache. After an hour a datasource's
answer may have changed, and the user chooses among the candidates in the fresh answer.

Finding the candidates is a synchronous resolution. It is a run like any other, so its
fetches record fetch keys and its findings belong to it. It executes in the caller and
responds with the candidates. It shares its lookups, fetches and checks against the
stated key with a resolution run. It stops before choosing, and lists every group that
survived the checks, ordered by datasource precedence and then by each provider's own
order.

Confirming is a second synchronous resolution. The user's choice names the datasource and
a stable identifier of the group, or a MIC_TICKER where the group lacks a stable
identifier. Resolution reads the answers from the fetch cache of
[011](011-fetches-are-cached-for-an-hour.md), or fetches again where an entry expired. It
takes the group that the choice names as the winner, and writes it as a resolution run
writes a winner. Groups of other datasources attach to it by the usual rules. The
instrument data comes from the datasource's answer, with system authority. Only the
choice is the user's. Where the answer lacks the chosen group, the confirmation fails and
the user is shown the candidates again.

## Considered options

Writing every candidate as an instrument when the key is first resolved would let the
candidates be read back from the database. It writes instruments nobody holds, records
coverage for each, and runs the full write path once per candidate.
