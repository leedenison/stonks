// Package google is the Google Sheets tool: the command line client through
// which an administrator asks GOOGLEFINANCE, a function of Google Sheets, for
// the prices that the other datasources leave as gaps.
//
// Google does not serve historical GOOGLEFINANCE values through the Sheets
// API, so a round ends with a manual step. The write step puts one formula
// for each series into the formulas tab of a spreadsheet the tool created.
// The administrator opens the spreadsheet, waits for it to evaluate, and
// pastes the values of the whole formulas tab into the values tab.
//
// The tool is a client of the service and of Google. It reaches the service
// through its API alone.
//
// A formula names the current ticker of a listing, because Google serves the
// history of a security under the symbol as it stands today.
package google
