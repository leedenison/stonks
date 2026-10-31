# Alerts rank by kind, then by what rests on them

One list must order alerts of different kinds. A score across kinds would be a guess
until the page has been used on real data, so the order is fixed by kind: failed run,
block, ticker that names nothing, ticker validity gap, corporate action, finding. Within
a kind the server orders by the transactions that rest on the alert where the subject
counts them, then by the subject's date, then by a natural key.

The home view is a digest: the count of each kind and its top five, from one RPC. The
nav badge reads the same digest. A kind's full list is a second RPC that requires the
kind and pages by keyset cursor, as the other admin lists do. One bounded query per
kind keeps each page cheap, and a cursor over one kind survives rows that appear or
vanish between pages. The ticker that names nothing and the ticker validity gap are the
exception: gap derivation runs in Go over stored ticker validity, so each read derives
both kinds in full and cuts the page in memory.
