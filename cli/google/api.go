package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// endpoint is the address of the Sheets API.
const endpoint = "https://sheets.googleapis.com"

// The two tabs of the spreadsheet.
const (
	formulasTab = "formulas"
	valuesTab   = "values"
)

// zone is the time zone of the spreadsheet. UTC makes the date cell state
// the UTC day of the evaluation.
const zone = "Etc/GMT"

// Sheets calls the Sheets API.
type Sheets struct {
	http *http.Client
	base string
}

// NewSheets returns a client that sends through h, which carries the
// credentials. An empty base is the address of Google.
func NewSheets(h *http.Client, base string) *Sheets {
	if base == "" {
		base = endpoint
	}
	return &Sheets{http: h, base: base}
}

// statusError is an answer of the Sheets API other than 200.
type statusError struct {
	Status int
	Body   string
}

func (e statusError) Error() string {
	return fmt.Sprintf("the Sheets API answered %d: %s", e.Status, e.Body)
}

// do sends one call and decodes its answer into out, where out is set.
func (s *Sheets) do(ctx context.Context, method, path string, query url.Values, in, out any) (err error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	u := s.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, res.Body.Close()) }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return statusError{Status: res.StatusCode, Body: string(b)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

type gridProperties struct {
	RowCount    int `json:"rowCount"`
	ColumnCount int `json:"columnCount"`
}

type sheetProperties struct {
	SheetID        int             `json:"sheetId"`
	Title          string          `json:"title,omitempty"`
	GridProperties *gridProperties `json:"gridProperties,omitempty"`
}

type sheet struct {
	Properties sheetProperties `json:"properties"`
}

type spreadsheet struct {
	SpreadsheetID string `json:"spreadsheetId,omitempty"`
	Properties    struct {
		Title    string `json:"title"`
		TimeZone string `json:"timeZone"`
	} `json:"properties"`
	Sheets []sheet `json:"sheets"`
}

// tabIDs holds the id Google gave each tab of the spreadsheet, by title.
type tabIDs map[string]int

// tabs reads the ids of the two tabs, and reports false where either is gone.
func (s spreadsheet) tabs() (tabIDs, bool) {
	ids := tabIDs{}
	for _, sh := range s.Sheets {
		ids[sh.Properties.Title] = sh.Properties.SheetID
	}
	_, formulas := ids[formulasTab]
	_, values := ids[valuesTab]
	return ids, formulas && values
}

// create makes the spreadsheet of the tool, with its two tabs, and returns
// its id and theirs.
func (s *Sheets) create(ctx context.Context, title string) (string, tabIDs, error) {
	in := spreadsheet{}
	in.Properties.Title, in.Properties.TimeZone = title, zone
	in.Sheets = []sheet{
		{Properties: sheetProperties{SheetID: 1, Title: formulasTab}},
		{Properties: sheetProperties{SheetID: 2, Title: valuesTab}},
	}
	var out spreadsheet
	if err := s.do(ctx, http.MethodPost, "/v4/spreadsheets", nil, in, &out); err != nil {
		return "", nil, fmt.Errorf("create the spreadsheet: %w", err)
	}
	ids, ok := out.tabs()
	if out.SpreadsheetID == "" || !ok {
		return "", nil, errors.New("create the spreadsheet: the answer lacks its id or its tabs")
	}
	return out.SpreadsheetID, ids, nil
}

// tabs returns the ids of the two tabs of the spreadsheet id. It reports
// false when the tool can no longer use the spreadsheet: its owner deleted
// it, which answers 404, or deleted one of the tabs.
func (s *Sheets) tabs(ctx context.Context, id string) (tabIDs, bool, error) {
	var out spreadsheet
	err := s.do(ctx, http.MethodGet, "/v4/spreadsheets/"+url.PathEscape(id), url.Values{"fields": {"sheets.properties(sheetId,title)"}}, nil, &out)
	var status statusError
	if errors.As(err, &status) && status.Status == http.StatusNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read the spreadsheet: %w", err)
	}
	ids, ok := out.tabs()
	return ids, ok, nil
}

// resize sets the grid of both tabs. An administrator copies a whole tab,
// and a browser runs out of memory on a tab of millions of cells, whether or
// not they hold a value. So each tab is as large as its round and no larger.
func (s *Sheets) resize(ctx context.Context, id string, tabs tabIDs, rows, columns int) error {
	var requests []map[string]any
	for _, tab := range []string{formulasTab, valuesTab} {
		requests = append(requests, map[string]any{"updateSheetProperties": map[string]any{
			"properties": sheetProperties{SheetID: tabs[tab], GridProperties: &gridProperties{RowCount: rows, ColumnCount: columns}},
			"fields":     "gridProperties(rowCount,columnCount)",
		}})
	}
	in := map[string]any{"requests": requests}
	if err := s.do(ctx, http.MethodPost, "/v4/spreadsheets/"+url.PathEscape(id)+":batchUpdate", nil, in, nil); err != nil {
		return fmt.Errorf("resize the spreadsheet: %w", err)
	}
	return nil
}

// clear empties both tabs.
func (s *Sheets) clear(ctx context.Context, id string) error {
	in := map[string]any{"ranges": []string{formulasTab, valuesTab}}
	if err := s.do(ctx, http.MethodPost, "/v4/spreadsheets/"+url.PathEscape(id)+"/values:batchClear", nil, in, nil); err != nil {
		return fmt.Errorf("clear the spreadsheet: %w", err)
	}
	return nil
}

// userEntered has the Sheets API parse each cell it is given as if it were
// typed, so a formula evaluates.
const userEntered = "USER_ENTERED"

// write puts cells into tab from its first cell.
func (s *Sheets) write(ctx context.Context, id, tab, input string, cells [][]any) error {
	in := map[string]any{
		"valueInputOption": input,
		"data":             []map[string]any{{"range": tab + "!A1", "values": cells}},
	}
	if err := s.do(ctx, http.MethodPost, "/v4/spreadsheets/"+url.PathEscape(id)+"/values:batchUpdate", nil, in, nil); err != nil {
		return fmt.Errorf("write the %s tab: %w", tab, err)
	}
	return nil
}

// unformatted has the Sheets API return each value as stored.
const unformatted = "UNFORMATTED_VALUE"

// read returns the cells of tab, with each value rendered by the render
// option and each date as a serial number. The API leaves out the empty cells
// at the end of each row and the empty rows at the end.
func (s *Sheets) read(ctx context.Context, id, tab, render string) ([][]any, error) {
	q := url.Values{"valueRenderOption": {render}, "dateTimeRenderOption": {"SERIAL_NUMBER"}}
	var out struct {
		Values [][]any `json:"values"`
	}
	if err := s.do(ctx, http.MethodGet, "/v4/spreadsheets/"+url.PathEscape(id)+"/values/"+url.PathEscape(tab), q, nil, &out); err != nil {
		return nil, fmt.Errorf("read the %s tab: %w", tab, err)
	}
	return out.Values, nil
}
