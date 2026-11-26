package google

import (
	"context"
	"fmt"
	"io"
	"time"

	"connectrpc.com/connect"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
)

// title is the name of the spreadsheet in the Drive of the administrator.
const title = "Stonks prices"

// Tool runs the write step of a round.
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
		"Wait until every formula has evaluated. Select the whole %q tab (Ctrl+A), copy it, and paste its values only (Ctrl+Shift+V) into cell A1 of the %q tab.\n",
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
