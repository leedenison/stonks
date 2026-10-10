# A listing is named at the primary venue a datasource states

A listing carries a ticker at every venue some provider named, and a holding is shown
to the user by one of them. The listing records its primary venue, `primary_mic`, the
operating MIC a datasource stated for it. Massive states one for every record it
returns, and EODHD marks the primary row of a search. OpenFIGI states none.

The first datasource to state a primary venue sets it, and the value then stays fixed.
This is a deliberate exception to
[001](001-the-seeded-precedence-is-openfigi-massive-eodhd.md), under which precedence
alone decides what a provider supplies. A primary venue is a fact that providers rarely
dispute, and a flip between runs would rename the holding. Within one run the groups
attach in precedence order, so the highest precedence datasource stating one wins there.

Where no datasource has stated a primary venue, the listing is named at the first of
its venues by a fixed rank, and by MIC where none is ranked. The rank places the
national exchanges first and NYSE above Nasdaq, so a listing OpenFIGI describes at both
with no stated primary is named at NYSE, which Massive corrects for a US listing it
covers.

Each venue's common name and rank are reference data beside the MIC and currency
tables. They live in a migration rather than the dev seed. A venue outside the table is
shown by its MIC.
