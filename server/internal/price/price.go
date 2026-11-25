// Package price stores and reads the end of day prices of listings.
//
// The rate of a currency is the price of the USD listing of that currency's
// instrument: the number of USD that one unit of the currency buys. Prices
// and rates share every rule of this package but restatement.
//
// A datasource answers for a period, and the period is stored as coverage
// whether or not the answer held a price for each day. A covered day without
// a price had no session. A day that no datasource covers is unknown.
//
// The first answer of a datasource for a listing and a day stands. A write
// keeps only the days of an answer that the datasource has not yet covered,
// so the coverage periods of one datasource for one listing never overlap.
//
// The prices the system needs are those of each day on which a user holds a
// priced instrument, from a week before the holding opens to the last
// completed day, which is the UTC day before today. A holding demands the
// listing its key names, or every listing of the instrument where the key
// names only the instrument; see [tickers.go](../identity/tickers/tickers.go).
// An option, a future and a key without an instrument demand nothing.
//
// A currency is in use when it is the currency of a demanded listing or of a
// cash holding. The rate of each currency in use is needed over one period,
// from a week before the first transaction of any user to the last completed
// day.
//
// A gap is a needed day that no datasource covers. It is the same for every
// datasource. A run asks the datasources in turn, and each answer shrinks the
// gap that the next one is asked for. The gaps are the only record of what to
// ask again, so a failed request or a refused answer leaves its days to the
// next run.
//
// A read takes, for each day, the price of the datasource that ranks first,
// so a change of precedence applies to the stored prices at once. It restates
// the price of an instrument whose holdings corporate actions restate, from
// its as at day to today, through the calendar of the instrument; see
// [calendar.go](../corpact/calendar.go). Such a price is provisional where
// the calendar does not cover every day from its as at day to today. The
// price of any other class, and every rate, is read as stored.
//
// A covered day without a price takes the latest earlier close, whichever
// datasource served it. The coverage of every datasource together must be
// unbroken from the day of that close to the covered day. Otherwise the day
// is unknown.
package price
