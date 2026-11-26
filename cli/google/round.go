package google

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"connectrpc.com/connect"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
)

// title is the name of the spreadsheet in the Drive of the administrator.
const title = "Stonks prices"

// chunk is the most prices one upload carries. A series is never split, so
// an upload holds at least one series whatever its length.
const chunk = 20_000

// Tool runs the steps of a round.
type Tool struct {
	Sheets *Sheets
	Prices adminpricev1connect.PriceServiceClient
	// Dir is the state directory.
	Dir   string
	Clock func() time.Time
	// Out receives the report of each step.
	Out io.Writer
}

// address is the web address of the spreadsheet id.
func address(id string) string {
	return "https://docs.google.com/spreadsheets/d/" + id
}

// report prints one line for each series left out.
func (t *Tool) report(skipped []skip) error {
	for _, s := range skipped {
		if _, err := fmt.Fprintf(t.Out, "skipped %s: %s\n", s.Name, s.Reason); err != nil {
			return err
		}
	}
	return nil
}

// Write asks the service for the gaps and writes a formula for each series
// into the formulas tab. It creates the spreadsheet on its first use and
// reuses it afterwards. It reports each gap it left out. When it has a
// series to write, it empties the values tab, so the paste of an earlier
// round does not survive.
func (t *Tool) Write(ctx context.Context) error {
	res, err := t.Prices.ListPriceGaps(ctx, connect.NewRequest(&adminpricev1.ListPriceGapsRequest{}))
	if err != nil {
		return fmt.Errorf("list the price gaps: %w", err)
	}
	today := t.Clock()
	blocks, skipped := plan(res.Msg.GetGaps(), today)
	for _, u := range res.Msg.GetUnasked() {
		skipped = append(skipped, skip{
			Name:   "listing " + u.GetListingId(),
			Reason: fmt.Sprintf("the %s listing of instrument %s lacks a current ticker", u.GetCurrency(), u.GetInstrId()),
		})
	}
	if err := t.report(skipped); err != nil {
		return err
	}
	if len(blocks) == 0 {
		_, err := fmt.Fprintf(t.Out, "nothing to write: 0 series, %d skipped\n", len(skipped))
		return err
	}
	id, tabs, err := t.spreadsheet(ctx)
	if err != nil {
		return err
	}
	if err := t.Sheets.clear(ctx, id); err != nil {
		return err
	}
	rows, columns := size(blocks, today)
	if err := t.Sheets.resize(ctx, id, tabs, rows, columns); err != nil {
		return err
	}
	if err := t.Sheets.write(ctx, id, formulasTab, userEntered, cells(blocks, today)); err != nil {
		return err
	}
	series := map[string]bool{}
	for _, b := range blocks {
		series[b.Listing] = true
	}
	_, err = fmt.Fprintf(t.Out, "wrote %d series in %d formulas, %d skipped\n\n"+
		"Open %s\n"+
		"Wait until every formula has evaluated. Select the whole %q tab (Ctrl+A), copy it, and paste its values only (Ctrl+Shift+V) into cell A1 of the %q tab. Then run the read step.\n",
		len(series), len(blocks), len(skipped), address(id), formulasTab, valuesTab)
	return err
}

// spreadsheet returns the spreadsheet of the tool and the ids of its tabs.
// It creates one when the state names none, or names one the tool can no
// longer use.
func (t *Tool) spreadsheet(ctx context.Context) (string, tabIDs, error) {
	s, err := load(t.Dir)
	if err != nil {
		return "", nil, err
	}
	if s.SpreadsheetID != "" {
		tabs, ok, err := t.Sheets.tabs(ctx, s.SpreadsheetID)
		if err != nil {
			return "", nil, err
		}
		if ok {
			return s.SpreadsheetID, tabs, nil
		}
	}
	id, tabs, err := t.Sheets.create(ctx, title)
	if err != nil {
		return "", nil, err
	}
	s.SpreadsheetID = id
	if err := s.save(t.Dir); err != nil {
		return "", nil, err
	}
	return id, tabs, nil
}

// refusals says why the service refused a series.
var refusals = map[adminpricev1.RefusalReason]string{
	adminpricev1.RefusalReason_REFUSAL_REASON_CURRENCY:        "the currency Google quoted is outside the family of the listing",
	adminpricev1.RefusalReason_REFUSAL_REASON_INVALID_PRICE:   "a price is zero, or is dated after the evaluation",
	adminpricev1.RefusalReason_REFUSAL_REASON_UNKNOWN_LISTING: "the service lacks the listing",
}

// Read reads the values tab and uploads each series to the service. It
// reports each series it left out and each one the service refused, and
// returns an error when there is any, after it has uploaded the others.
func (t *Tool) Read(ctx context.Context) error {
	s, err := load(t.Dir)
	if err != nil {
		return err
	}
	if s.SpreadsheetID == "" {
		return errors.New("the tool has no spreadsheet yet: run the write step first")
	}
	cells, err := t.Sheets.read(ctx, s.SpreadsheetID, valuesTab, formula)
	if err != nil {
		return err
	}
	asAt, series, skipped, err := parse(cells)
	if err != nil {
		return err
	}
	if err := t.report(skipped); err != nil {
		return err
	}
	symbols := map[string]string{}
	for _, s := range series {
		symbols[s.GetListingId()] = s.GetSymbol()
	}
	refused := 0
	for len(series) > 0 {
		n, prices := 0, 0
		for n < len(series) && (n == 0 || prices+len(series[n].GetPrices()) <= chunk) {
			prices += len(series[n].GetPrices())
			n++
		}
		res, err := t.Prices.UploadPrices(ctx, connect.NewRequest(&adminpricev1.UploadPricesRequest{AsAt: asAt.Format(time.DateOnly), Series: series[:n]}))
		if err != nil {
			return fmt.Errorf("upload the prices: %w", err)
		}
		series = series[n:]
		for _, r := range res.Msg.GetRefused() {
			refused++
			reason, ok := refusals[r.GetReason()]
			if !ok {
				reason = r.GetReason().String()
			}
			if _, err := fmt.Fprintf(t.Out, "refused %s: %s\n", symbols[r.GetListingId()], reason); err != nil {
				return err
			}
		}
	}
	stored := len(symbols) - refused
	if _, err := fmt.Fprintf(t.Out, "uploaded %d series as at %s, %d refused, %d skipped\n", stored, asAt.Format(time.DateOnly), refused, len(skipped)); err != nil {
		return err
	}
	if refused+len(skipped) > 0 {
		return fmt.Errorf("%d series were refused or skipped", refused+len(skipped))
	}
	return nil
}
