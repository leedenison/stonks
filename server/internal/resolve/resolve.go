// Package resolve resolves stated keys to system owned instruments.
//
// A key is resolved through the identifiers it states that are recognised
// globally: eg. an ISIN, a SEDOL or a ticker with its venue. A key stating
// only a bare ticker never associates through it.
//
// Choice. Every datasource serves a key with candidates. The candidates of
// one response are grouped by the instrument grain identifiers they share,
// transitively, so each group is one instrument. Within a group the
// candidates collapse to one listing per currency family. Groups compete,
// the datasources taken in precedence order and the instrument the database
// already holds taken first. The top group of the highest precedence
// datasource is the winner.
package resolve
