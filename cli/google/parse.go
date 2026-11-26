package google

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
)

// epoch is day zero of the serial numbers in which Google Sheets counts
// dates.
var epoch = time.Date(1899, time.December, 30, 0, 0, 0, 0, time.UTC)

// The header Google puts above the rows of a historical answer, whatever the
// attribute asked.
const (
	dateHeader  = "Date"
	closeHeader = "Close"
)

// ratePrefix starts the symbol of every rate.
const ratePrefix = "CURRENCY:"

// cell returns the cell at row r and column c. The Sheets API leaves out the
// empty cells at the end of a row, so a missing cell is the empty string.
func cell(cells [][]any, r, c int) any {
	if r >= len(cells) || c >= len(cells[r]) {
		return ""
	}
	return cells[r][c]
}

func str(cells [][]any, r, c int) string {
	s, _ := cell(cells, r, c).(string)
	return s
}

// dayOf reads a cell that holds a moment as the day it falls on. Google
// stamps each row with the close of its session in the time of the exchange,
// so the whole part of the serial number is the day of the session.
func dayOf(v any) (time.Time, bool) {
	n, ok := v.(float64)
	if !ok || n < 1 {
		return time.Time{}, false
	}
	return epoch.AddDate(0, 0, int(math.Floor(n))), true
}

// answered is a block with the answer Google gave for it.
type answered struct {
	block
	// currency is the code Google quoted, or the error it showed in place of
	// one.
	currency string
	closes   []*adminpricev1.DayPrice
	first    time.Time
	last     time.Time
}

// parse reads the values tab as an administrator pasted it. It returns the
// day of the evaluation and the series of each listing, and names each
// listing it leaves out. A listing takes the first of its candidate symbols
// that answered, in the order the write step gave them.
//
// The cells are read with formulas shown as text, so parse can tell a cell
// that still holds a formula from a pasted value. It refuses a tab when any
// cell holds a formula or when the date cell is missing.
func parse(cells [][]any) (asAt time.Time, series []*adminpricev1.PriceSeries, skipped []skip, err error) {
	for _, row := range cells {
		for _, v := range row {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "=") {
				return time.Time{}, nil, nil, fmt.Errorf("the %s tab holds the formula %s: paste the values only", valuesTab, s)
			}
		}
	}
	asAt, ok := dayOf(cell(cells, dateRow, 0))
	if !ok {
		return time.Time{}, nil, nil, fmt.Errorf("the %s tab lacks the date in its first cell: paste the whole %s tab into cell A1", valuesTab, formulasTab)
	}
	var order []string
	candidates := map[string][]answered{}
	for c := 0; str(cells, listingRow, c) == "listing"; c += 2 {
		a, err := readBlock(cells, c)
		if err != nil {
			return time.Time{}, nil, nil, fmt.Errorf("the block of %s: %w", str(cells, symbolRow, c+1), err)
		}
		if _, seen := candidates[a.Listing]; !seen {
			order = append(order, a.Listing)
		}
		candidates[a.Listing] = append(candidates[a.Listing], a)
	}
	if len(order) == 0 {
		return time.Time{}, nil, nil, fmt.Errorf("the %s tab holds no series", valuesTab)
	}
	for _, listing := range order {
		var symbols []string
		var took *answered
		for _, a := range candidates[listing] {
			symbols = append(symbols, a.Symbol)
			if took == nil && len(a.closes) > 0 {
				took = &a
			}
		}
		name := strings.Join(symbols, ", ")
		if took == nil {
			skipped = append(skipped, skip{Name: name, Reason: "Google answered under none of the symbols"})
			continue
		}
		if !took.Rate && !isCode(took.currency) {
			skipped = append(skipped, skip{Name: took.Symbol, Reason: "Google states no currency: " + took.currency})
			continue
		}
		s := took.series(asAt)
		if len(s.GetCoverage()) == 0 {
			skipped = append(skipped, skip{Name: took.Symbol, Reason: "Google answered for no day of the gaps before the evaluation"})
			continue
		}
		series = append(series, s)
	}
	return asAt, series, skipped, nil
}

// isCode reports whether s has the shape of a currency code, which an error
// shown in the currency cell lacks.
func isCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// readBlock reads the block whose first column is c.
func readBlock(cells [][]any, c int) (answered, error) {
	a := answered{block: block{Listing: str(cells, listingRow, c+1), Symbol: str(cells, symbolRow, c+1)}, currency: str(cells, currencyRow, c+1)}
	a.Rate = strings.HasPrefix(a.Symbol, ratePrefix)
	if a.Listing == "" || a.Symbol == "" {
		return a, errors.New("it lacks its listing or its symbol")
	}
	for _, f := range strings.Fields(str(cells, periodsRow, c+1)) {
		from, before, ok := strings.Cut(f, "/")
		p, err := periodOf(&typev1.Period{From: &from, Before: &before})
		if !ok || err != nil || !p.From.Before(p.Before) {
			return a, fmt.Errorf("its period %q is not two days in order", f)
		}
		a.Periods = append(a.Periods, p)
	}
	if len(a.Periods) == 0 {
		return a, errors.New("it lacks its periods")
	}
	// A symbol Google does not know shows an error in place of the header.
	if str(cells, seriesRow, c) != dateHeader || str(cells, seriesRow, c+1) != closeHeader {
		return a, nil
	}
	for r := seriesRow + 1; ; r++ {
		d, ok := dayOf(cell(cells, r, c))
		if !ok {
			break
		}
		price, ok := cell(cells, r, c+1).(float64)
		if !ok || price <= 0 {
			return a, fmt.Errorf("the price on %s is %v", d.Format(time.DateOnly), cell(cells, r, c+1))
		}
		if len(a.closes) == 0 {
			a.first = d
		}
		a.last = d
		a.closes = append(a.closes, &adminpricev1.DayPrice{Day: d.Format(time.DateOnly), Price: strconv.FormatFloat(price, 'f', -1, 64)})
	}
	return a, nil
}

// series states what Google answered for the gaps of a. The coverage of each
// gap runs from the first row to the last, since Google does not say where
// its history of a symbol starts or ends. The formula asked a margin beyond
// each gap, so a row in the margin lets the days without a session at the
// edge of a gap count as covered. Coverage ends before the day of the
// evaluation, whose close may be unpublished.
func (a answered) series(asAt time.Time) *adminpricev1.PriceSeries {
	out := &adminpricev1.PriceSeries{ListingId: a.Listing, Symbol: a.Symbol, Currency: a.currency}
	if a.Rate {
		out.Currency = base
	}
	served := period{From: a.first, Before: a.last.AddDate(0, 0, 1)}
	if served.Before.After(asAt) {
		served.Before = asAt
	}
	var covered []period
	for _, p := range a.Periods {
		if p.From.Before(served.From) {
			p.From = served.From
		}
		if p.Before.After(served.Before) {
			p.Before = served.Before
		}
		if !p.From.Before(p.Before) {
			continue
		}
		covered = append(covered, p)
		from, before := p.From.Format(time.DateOnly), p.Before.Format(time.DateOnly)
		out.Coverage = append(out.Coverage, &typev1.Period{From: &from, Before: &before})
	}
	for _, c := range a.closes {
		d, err := time.Parse(time.DateOnly, c.GetDay())
		if err != nil {
			continue
		}
		for _, p := range covered {
			if !d.Before(p.From) && d.Before(p.Before) {
				out.Prices = append(out.Prices, c)
				break
			}
		}
	}
	return out
}
