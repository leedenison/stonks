package google

import (
	"fmt"
	"slices"
	"strings"
	"time"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
)

// margin is how many days a formula asks beyond each end of the gaps of its
// series. A session beyond a gap lets the days without a session at the edge
// of the gap count as covered.
const margin = 7

// budget is the most cells a tab of one round may hold. The administrator
// copies the whole tab, and a tab of 4.8 million cells ran the browser out
// of memory. The budget is set well under that and is otherwise untuned.
const budget = 500_000

// base is the currency against which the service stores every rate.
const base = "USD"

// The attribute a historical formula asks for.
const (
	closeAttribute = "close"
	rateAttribute  = "price"
)

// The rows of the formulas tab. The date cell is the first cell of the tab.
// Each block labels its next four rows in its first column and states the
// value in its second. The historical formula sits in the first column of
// the row after, and its answer spills below and to the right.
//
// The rows touch, without an empty row between them. Select all in Google
// Sheets takes the cells that touch the active cell, so a gap would leave
// the date cell out of the copy.
const (
	dateRow = iota
	listingRow
	symbolRow
	periodsRow
	currencyRow
	seriesRow
)

// period is a half open range of days.
type period struct {
	From, Before time.Time
}

func (p period) String() string {
	return p.From.Format(time.DateOnly) + "/" + p.Before.Format(time.DateOnly)
}

// block is one series under one candidate symbol: two columns of a tab.
type block struct {
	Listing string
	// Symbol is the name Google is asked under, such as "NASDAQ:AAPL" or
	// "CURRENCY:EURUSD".
	Symbol string
	// Rate marks the rate of a currency, which Google quotes in USD.
	Rate bool
	// Periods are the gaps of the listing.
	Periods []period
}

// asked returns the one period the formula of b asks for: from the earliest
// gap to the latest, widened by the margin and cut to end before today. It
// reports false when the period is empty.
func (b block) asked(today time.Time) (period, bool) {
	p := period{From: b.Periods[0].From, Before: b.Periods[0].Before}
	for _, q := range b.Periods[1:] {
		if q.From.Before(p.From) {
			p.From = q.From
		}
		if q.Before.After(p.Before) {
			p.Before = q.Before
		}
	}
	p.From, p.Before = p.From.AddDate(0, 0, -margin), p.Before.AddDate(0, 0, margin)
	if p.Before.After(today) {
		p.Before = today
	}
	return p, p.From.Before(p.Before)
}

// skip is a series a step left out, with the reason.
type skip struct {
	// Name identifies the series: its identifier, or its listing where it
	// has none.
	Name   string
	Reason string
}

func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func periodOf(p *typev1.Period) (period, error) {
	from, err := time.Parse(time.DateOnly, p.GetFrom())
	if err != nil {
		return period{}, err
	}
	before, err := time.Parse(time.DateOnly, p.GetBefore())
	if err != nil {
		return period{}, err
	}
	return period{From: from, Before: before}, nil
}

// rows returns how many rows of a tab the block needs: its header rows, the
// header of the answer, and one row for each day from the first asked to the
// end date of the formula, since a rate has a row for every calendar day.
func (b block) rows(today time.Time) int {
	asked, _ := b.asked(today)
	return seriesRow + 3 + int(asked.Before.Sub(asked.From).Hours()/24)
}

// size returns the grid that holds the blocks: the rows of the longest and
// two columns for each.
func size(blocks []block, today time.Time) (rows, columns int) {
	today = day(today)
	for _, b := range blocks {
		rows = max(rows, b.rows(today))
	}
	return rows, 2 * len(blocks)
}

// plan turns the gaps into the blocks of one round, and reports the gaps it
// leaves out. A listing keeps its candidate blocks side by side. A listing
// whose blocks would take the tab past its budget of cells waits for the
// next round, and those are reported as one count. The first listing is
// always taken, so a round never stalls on one long series.
func plan(gaps []*adminpricev1.PriceGap, today time.Time) (blocks []block, skipped []skip) {
	today = day(today)
	waiting := 0
	for _, g := range gaps {
		id := g.GetIdentifier()
		name := id.GetDomain() + ":" + id.GetValue()
		var symbols []string
		rate := id.GetType() == typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY
		switch {
		case rate:
			name = id.GetValue()
			symbols = []string{"CURRENCY:" + id.GetValue() + base}
		case id.GetType() == typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER:
			for _, code := range Codes[id.GetDomain()] {
				symbols = append(symbols, code+":"+id.GetValue())
			}
			if len(symbols) == 0 {
				skipped = append(skipped, skip{Name: name, Reason: fmt.Sprintf("the tool maps the venue %s to no exchange code of Google", id.GetDomain())})
				continue
			}
		default:
			skipped = append(skipped, skip{Name: name, Reason: "the gap is asked under an identifier that is neither a ticker nor a currency"})
			continue
		}
		var periods []period
		for _, p := range g.GetPeriods() {
			q, err := periodOf(p)
			if err != nil {
				periods = nil
				break
			}
			periods = append(periods, q)
		}
		if len(periods) == 0 {
			skipped = append(skipped, skip{Name: name, Reason: "the gap states no period the tool can read"})
			continue
		}
		if _, ok := (block{Periods: periods}).asked(today); !ok {
			skipped = append(skipped, skip{Name: name, Reason: "the gap holds no day before today"})
			continue
		}
		var added []block
		for _, s := range symbols {
			added = append(added, block{Listing: g.GetListingId(), Symbol: s, Rate: rate, Periods: periods})
		}
		if rows, columns := size(append(slices.Clone(blocks), added...), today); len(blocks) > 0 && rows*columns > budget {
			waiting++
			continue
		}
		blocks = append(blocks, added...)
	}
	if waiting > 0 {
		skipped = append(skipped, skip{Name: fmt.Sprintf("%d series", waiting), Reason: "the spreadsheet is full, so they wait for the next round"})
	}
	return blocks, skipped
}

// text marks a cell as text for an input that parses cells as typed.
func text(s string) string { return "'" + s }

func date(t time.Time) string {
	return fmt.Sprintf("DATE(%d,%d,%d)", t.Year(), int(t.Month()), t.Day())
}

// cells lays the blocks out as the formulas tab. Every row has the width of
// the tab in use.
func cells(blocks []block, today time.Time) [][]any {
	today = day(today)
	width := max(2*len(blocks), 1)
	out := make([][]any, seriesRow+1)
	for r := range out {
		out[r] = slices.Repeat([]any{""}, width)
	}
	out[dateRow][0] = "=TODAY()"
	for i, b := range blocks {
		c := 2 * i
		ps := make([]string, len(b.Periods))
		for j, p := range b.Periods {
			ps[j] = p.String()
		}
		asked, _ := b.asked(today)
		attribute, currency := closeAttribute, any(fmt.Sprintf(`=GOOGLEFINANCE(%q,"currency")`, b.Symbol))
		if b.Rate {
			attribute, currency = rateAttribute, text(base)
		}
		out[listingRow][c], out[listingRow][c+1] = "listing", text(b.Listing)
		out[symbolRow][c], out[symbolRow][c+1] = "symbol", text(b.Symbol)
		out[periodsRow][c], out[periodsRow][c+1] = "periods", text(strings.Join(ps, " "))
		out[currencyRow][c], out[currencyRow][c+1] = "currency", currency
		// Google answers a security through its end date and a rate only to
		// the day before it. The formula gives the day after the last it
		// needs as its end, which serves both. A row on that day is outside
		// the gaps, and the read step leaves it out.
		out[seriesRow][c] = fmt.Sprintf(`=GOOGLEFINANCE(%q,%q,%s,%s,"DAILY")`, b.Symbol, attribute, date(asked.From), date(asked.Before))
	}
	return out
}
