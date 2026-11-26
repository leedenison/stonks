package google

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
)

// call is one request the tool sent to the Sheets API.
type call struct {
	Method, Path string
	Body         map[string]any
}

// sheets stands in for the Sheets API and keeps each call.
type sheets struct {
	*httptest.Server
	calls []call
	// gone answers 404 for the spreadsheet named, as a deleted one does.
	gone string
	// tabless answers for the spreadsheet named without its values tab, as
	// one whose administrator deleted the tab does.
	tabless string
}

// Google's answers for a spreadsheet and its tabs.
const (
	bothTabs = `"sheets": [{"properties": {"sheetId": 11, "title": "formulas"}}, {"properties": {"sheetId": 12, "title": "values"}}]`
	oneTab   = `"sheets": [{"properties": {"sheetId": 11, "title": "formulas"}}]`
)

func newSheets(t *testing.T) *sheets {
	t.Helper()
	s := &sheets{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{Method: r.Method, Path: r.URL.Path}
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if len(b) > 0 {
			require.NoError(t, json.Unmarshal(b, &c.Body))
		}
		s.calls = append(s.calls, c)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets":
			_, err = io.WriteString(w, `{"spreadsheetId": "sheet-1", `+bothTabs+`}`)
		case s.gone != "" && strings.HasSuffix(r.URL.Path, "/"+s.gone):
			w.WriteHeader(http.StatusNotFound)
			_, err = io.WriteString(w, `{"error": {"code": 404}}`)
		case r.Method == http.MethodGet && s.tabless != "" && strings.HasSuffix(r.URL.Path, "/"+s.tabless):
			_, err = io.WriteString(w, `{`+oneTab+`}`)
		case r.Method == http.MethodGet:
			_, err = io.WriteString(w, `{`+bothTabs+`}`)
		default:
			_, err = io.WriteString(w, `{}`)
		}
		require.NoError(t, err)
	}))
	t.Cleanup(s.Close)
	return s
}

// paths lists the calls as method and path.
func (s *sheets) paths() []string {
	out := make([]string, len(s.calls))
	for i, c := range s.calls {
		out[i] = c.Method + " " + c.Path
	}
	return out
}

// tool returns a Tool over the stubs, signed in to svc, with its report.
func tool(t *testing.T, svc *service, sh *sheets, dir string) (*Tool, *bytes.Buffer) {
	t.Helper()
	srv := svc.serve(t)
	prices, err := SignIn(context.Background(), srv.Client(), srv.URL, "id-token")
	require.NoError(t, err)
	out := &bytes.Buffer{}
	return &Tool{Sheets: NewSheets(sh.Client(), sh.URL), Prices: prices, Dir: dir, Clock: func() time.Time { return today }, Out: out}, out
}

// TestWrite checks the calls of a first write step, what it writes, and what
// it reports for the gaps it leaves out.
func TestWrite(t *testing.T) {
	svc := &service{role: authv1.Role_ROLE_ADMIN, gaps: &adminpricev1.ListPriceGapsResponse{
		Gaps: []*adminpricev1.PriceGap{
			ticker("l1", "XLON", "VOD", span("2010-01-04", "2012-01-01")),
			ticker("l2", "XJPX", "7203", span("2010-01-04", "2012-01-01")),
		},
		Unasked: []*adminpricev1.UnaskedListing{{ListingId: "l3", InstrId: "i3", Currency: "GBP"}},
	}}
	sh, dir := newSheets(t), t.TempDir()
	require.NoError(t, state{RefreshToken: "refresh-1"}.save(dir))
	tl, out := tool(t, svc, sh, dir)
	require.NoError(t, tl.Write(context.Background()))

	wantCalls := []string{
		"POST /v4/spreadsheets",
		"POST /v4/spreadsheets/sheet-1/values:batchClear",
		"POST /v4/spreadsheets/sheet-1:batchUpdate",
		"POST /v4/spreadsheets/sheet-1/values:batchUpdate",
	}
	if diff := cmp.Diff(wantCalls, sh.paths()); diff != "" {
		t.Fatalf("calls mismatch (-want +got):\n%s", diff)
	}
	created := sh.calls[0].Body
	wantCreated := map[string]any{
		"properties": map[string]any{"title": "Stonks prices", "timeZone": "Etc/GMT"},
		"sheets": []any{
			map[string]any{"properties": map[string]any{"sheetId": 1.0, "title": "formulas"}},
			map[string]any{"properties": map[string]any{"sheetId": 2.0, "title": "values"}},
		},
	}
	if diff := cmp.Diff(wantCreated, created); diff != "" {
		t.Errorf("created spreadsheet mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[string]any{"ranges": []any{"formulas", "values"}}, sh.calls[1].Body); diff != "" {
		t.Errorf("cleared ranges mismatch (-want +got):\n%s", diff)
	}
	// Each tab is as large as the one series: six rows of headers, the
	// header of the answer, and a row for each of the 741 days asked.
	grid := func(sheetID float64) any {
		return map[string]any{"updateSheetProperties": map[string]any{
			"properties": map[string]any{"sheetId": sheetID, "gridProperties": map[string]any{"rowCount": 748.0, "columnCount": 2.0}},
			"fields":     "gridProperties(rowCount,columnCount)",
		}}
	}
	if diff := cmp.Diff(map[string]any{"requests": []any{grid(11), grid(12)}}, sh.calls[2].Body); diff != "" {
		t.Errorf("resized grids mismatch (-want +got):\n%s", diff)
	}
	// Only the mapped series has a block.
	wantWritten := map[string]any{
		"valueInputOption": "USER_ENTERED",
		"data": []any{map[string]any{"range": "formulas!A1", "values": []any{
			[]any{"=TODAY()", ""},
			[]any{"listing", "'l1"},
			[]any{"symbol", "'LON:VOD"},
			[]any{"periods", "'2010-01-04/2012-01-01"},
			[]any{"currency", `=GOOGLEFINANCE("LON:VOD","currency")`},
			[]any{`=GOOGLEFINANCE("LON:VOD","close",DATE(2009,12,28),DATE(2012,1,7),"DAILY")`, ""},
		}}},
	}
	if diff := cmp.Diff(wantWritten, sh.calls[3].Body); diff != "" {
		t.Errorf("written cells mismatch (-want +got):\n%s", diff)
	}
	wantReport := []string{
		"skipped XJPX:7203: the tool maps the venue XJPX to no exchange code of Google",
		"skipped listing l3: the GBP listing of instrument i3 lacks a current ticker",
		"wrote 1 series in 1 formulas, 2 skipped",
		"Open https://docs.google.com/spreadsheets/d/sheet-1",
	}
	for _, line := range wantReport {
		if !strings.Contains(out.String(), line+"\n") {
			t.Errorf("report lacks the line %q:\n%s", line, out)
		}
	}
	if got, want := stored(t, dir), (state{RefreshToken: "refresh-1", SpreadsheetID: "sheet-1"}); got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
}

// TestWriteReuses checks which spreadsheet a later write step uses.
func TestWriteReuses(t *testing.T) {
	gaps := &adminpricev1.ListPriceGapsResponse{Gaps: []*adminpricev1.PriceGap{ticker("l1", "XLON", "VOD", span("2010-01-04", "2012-01-01"))}}
	created := []string{
		"GET /v4/spreadsheets/sheet-0",
		"POST /v4/spreadsheets",
		"POST /v4/spreadsheets/sheet-1/values:batchClear",
		"POST /v4/spreadsheets/sheet-1:batchUpdate",
		"POST /v4/spreadsheets/sheet-1/values:batchUpdate",
	}
	tests := []struct {
		name      string
		gone      string
		tabless   string
		wantCalls []string
		wantID    string
	}{
		{
			name: "the spreadsheet of the state",
			wantCalls: []string{
				"GET /v4/spreadsheets/sheet-0",
				"POST /v4/spreadsheets/sheet-0/values:batchClear",
				"POST /v4/spreadsheets/sheet-0:batchUpdate",
				"POST /v4/spreadsheets/sheet-0/values:batchUpdate",
			},
			wantID: "sheet-0",
		},
		{name: "a new one when the administrator deleted it", gone: "sheet-0", wantCalls: created, wantID: "sheet-1"},
		{name: "a new one when the administrator deleted a tab", tabless: "sheet-0", wantCalls: created, wantID: "sheet-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &service{role: authv1.Role_ROLE_ADMIN, gaps: gaps}
			sh, dir := newSheets(t), t.TempDir()
			sh.gone, sh.tabless = tc.gone, tc.tabless
			require.NoError(t, state{RefreshToken: "refresh-1", SpreadsheetID: "sheet-0"}.save(dir))
			tl, _ := tool(t, svc, sh, dir)
			require.NoError(t, tl.Write(context.Background()))
			if diff := cmp.Diff(tc.wantCalls, sh.paths()); diff != "" {
				t.Errorf("calls mismatch (-want +got):\n%s", diff)
			}
			if got, want := stored(t, dir), (state{RefreshToken: "refresh-1", SpreadsheetID: tc.wantID}); got != want {
				t.Errorf("state = %+v, want %+v", got, want)
			}
		})
	}
}

// TestWriteNothing checks that a write step without a series it can ask for
// leaves the spreadsheet alone.
func TestWriteNothing(t *testing.T) {
	svc := &service{role: authv1.Role_ROLE_ADMIN, gaps: &adminpricev1.ListPriceGapsResponse{
		Gaps: []*adminpricev1.PriceGap{ticker("l2", "XJPX", "7203", span("2010-01-04", "2012-01-01"))},
	}}
	sh := newSheets(t)
	tl, out := tool(t, svc, sh, t.TempDir())
	require.NoError(t, tl.Write(context.Background()))
	if len(sh.calls) != 0 {
		t.Errorf("calls = %v, want none", sh.paths())
	}
	if !strings.Contains(out.String(), "nothing to write: 0 series, 1 skipped\n") {
		t.Errorf("report = %q, want it to say that nothing was written", out)
	}
}

// TestSheetsFailure checks that an answer other than 200 names its status
// and the step that failed.
func TestSheetsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error": {"status": "PERMISSION_DENIED"}}`, http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	s := NewSheets(srv.Client(), srv.URL)
	ctx := context.Background()
	if _, _, err := s.create(ctx, "t"); err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "PERMISSION_DENIED") {
		t.Errorf("create() error = %v, want the status and the body", err)
	}
	if _, _, err := s.tabs(ctx, "sheet-1"); err == nil {
		t.Error("tabs() error = nil for a 403, want an error and not a missing spreadsheet")
	}
	if _, err := s.read(ctx, "sheet-1", valuesTab, unformatted); err == nil || !strings.Contains(err.Error(), "values tab") {
		t.Errorf("read() error = %v, want one that names the tab", err)
	}
}

// TestRead checks the request of a read and the cells it returns.
func TestRead(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.RequestURI()
		_, err := io.WriteString(w, `{"range": "values!A1:B2", "values": [[46351, "x"], ["listing"]]}`)
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	cells, err := NewSheets(srv.Client(), srv.URL).read(context.Background(), "sheet-1", valuesTab, unformatted)
	require.NoError(t, err)
	if want := "GET /v4/spreadsheets/sheet-1/values/values?dateTimeRenderOption=SERIAL_NUMBER&valueRenderOption=UNFORMATTED_VALUE"; got != want {
		t.Errorf("request = %q, want %q", got, want)
	}
	if diff := cmp.Diff([][]any{{46351.0, "x"}, {"listing"}}, cells); diff != "" {
		t.Errorf("read() mismatch (-want +got):\n%s", diff)
	}
}
