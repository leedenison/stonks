package google

// Codes maps an operating MIC to the exchange codes under which Google lists
// its securities, in order of preference, from Google's list of exchanges at
// https://www.google.com/googlefinance/disclaimer/. One operating MIC can
// cover several of Google's exchanges, and the tool asks under each. A venue
// the map lacks is reported and its gap stays open.
//
// Tokyo is left out: Google refuses to serve the exchange to Google Sheets.
var Codes = map[string][]string{
	"BATS": {"BATS"},
	"XAMS": {"AMS"},
	"XASX": {"ASX"},
	"XFRA": {"ETR", "FRA"},
	"XHKG": {"HKG"},
	"XKRX": {"KRX", "KOSDAQ"},
	"XLON": {"LON"},
	"XNAS": {"NASDAQ"},
	"XNYS": {"NYSE", "NYSEARCA", "NYSEAMERICAN"},
	"XPAR": {"EPA"},
	"XSWX": {"SWX"},
	"XTSE": {"TSE"},
}
