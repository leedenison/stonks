// Package google is the Google Sheets tool: the command line client through
// which an administrator asks GOOGLEFINANCE, a function of Google Sheets, for
// the prices that the other datasources leave as gaps.
//
// Google does not reliably serve historical GOOGLEFINANCE values through the
// Sheets API. A round therefore has two steps with a manual one between
// them. The write step puts one formula for each series into the formulas
// tab of a spreadsheet the tool created. The administrator opens the
// spreadsheet, waits for it to evaluate, and pastes the values of the whole
// formulas tab into the values tab. The read step reads the values tab,
// which holds plain cells, and uploads each series to the service.
//
// The values tab describes itself: each series carries its listing, its
// symbol and its gaps beside its rows. Of what the write step made, the read
// step needs only the spreadsheet.
//
// Google adjusts the prices of a security for every split so far and leaves
// dividends out. The formulas tab carries one date cell. It states the day
// of the evaluation, and the upload states every price as at that day.
//
// The tool is a client of the service and of Google. It reaches the service
// through its API alone.
//
// A formula names the current ticker of a listing, because Google serves the
// history of a security under the symbol as it stands today.
package google
