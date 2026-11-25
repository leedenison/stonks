// Package price stores the end of day prices of listings.
//
// The rate of a currency is the price of the USD listing of that currency's
// instrument: the number of USD that one unit of the currency buys. Prices
// and rates share every rule of this package.
//
// A datasource answers for a period, and the period is stored as coverage
// whether or not the answer held a price for each day. A covered day without
// a price had no session. A day that no datasource covers is unknown.
//
// The first answer of a datasource for a listing and a day stands. A write
// keeps only the days of an answer that the datasource has not yet covered,
// so the coverage periods of one datasource for one listing never overlap.
package price
