package google

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/leedenison/stonks/internal/testutil/vcr"
	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
)

// recordDir is the environment variable that names the directory holding
// google.json, the state file of the tool after a sign-in, and client.json,
// the file of the OAuth desktop client. A recording reads both.
const recordDir = "STONKS_GOOGLE_RECORD_DIR"

// The id of a spreadsheet names a file in the Drive of whoever recorded, so
// it is replaced wherever the Sheets API carries it: in the path of each
// call, and in the body that answers a create.
var (
	inPath = regexp.MustCompile(`/v4/spreadsheets/[^/:?]+`)
	inBody = regexp.MustCompile(`"(spreadsheetId|spreadsheetUrl)":\s*"[^"]*"`)
)

var scrub = vcr.Scrub{
	Path: func(p string) string { return inPath.ReplaceAllString(p, "/v4/spreadsheets/"+vcr.Placeholder) },
	Body: func(b string) string { return inBody.ReplaceAllString(b, `"$1": "`+vcr.Placeholder+`"`) },
}

// creates counts the spreadsheets made through it.
type creates struct {
	n    int
	next http.RoundTripper
}

func (c *creates) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPost && r.URL.Path == "/v4/spreadsheets" {
		c.n++
	}
	return c.next.RoundTrip(r)
}

// recorded returns a Sheets client over a cassette, and the count of the
// spreadsheets it makes. A recording authenticates with a copy of the
// refresh token alone, so it never reads or changes the spreadsheet that the
// state of the tool names.
func recorded(t *testing.T, cassette string) (*Sheets, *creates) {
	t.Helper()
	path := "testdata/" + cassette
	h := vcr.New(t, path, scrub)
	if vcr.Recording(path) {
		dir := os.Getenv(recordDir)
		if dir == "" {
			t.Fatalf("%s is not set; a recording needs a sign-in", recordDir)
		}
		client, err := LoadClient(filepath.Join(dir, "client.json"))
		require.NoError(t, err)
		signedIn, err := load(dir)
		require.NoError(t, err)
		tmp := t.TempDir()
		require.NoError(t, state{RefreshToken: signedIn.RefreshToken}.save(tmp))
		source, err := Source(context.Background(), client, tmp)
		require.NoError(t, err)
		h.Transport = &oauth2.Transport{Source: source, Base: h.Transport}
	}
	count := &creates{next: h.Transport}
	h.Transport = count
	return NewSheets(h, ""), count
}

// round is the price gaps the stub service lists: one for each listing of
// the fixture. ZZZZQX is a symbol Google does not know.
var round = &adminpricev1.ListPriceGapsResponse{Gaps: []*adminpricev1.PriceGap{
	ticker("00000000-0000-0000-0000-0000000000a1", "XNAS", "AMZN", span("2026-11-07", "2026-11-14")),
	ticker("00000000-0000-0000-0000-0000000000a2", "XNYS", "IBM", span("2026-11-02", "2026-11-14")),
	ticker("00000000-0000-0000-0000-0000000000a3", "XLON", "VOD", span("2026-11-02", "2026-11-14")),
	rate("00000000-0000-0000-0000-0000000000a4", "GBP", span("2026-11-02", "2026-11-14")),
	ticker("00000000-0000-0000-0000-0000000000a5", "XNAS", "ZZZZQX", span("2026-11-02", "2026-11-14")),
}}

// recordedTool returns a Tool over the cassette and a stub of the service.
// The Tool starts with an empty state.
func recordedTool(t *testing.T, cassette string) (*Tool, *service, *creates) {
	t.Helper()
	svc := &service{role: authv1.Role_ROLE_ADMIN, gaps: round}
	srv := svc.serve(t)
	prices, err := SignIn(context.Background(), srv.Client(), srv.URL, "id-token")
	require.NoError(t, err)
	sh, count := recorded(t, cassette)
	// The clock reads the day after the fixture was evaluated. The read step
	// takes the as at day from the date cell and not from the clock.
	clock := func() time.Time { return time.Date(2026, time.November, 27, 9, 0, 0, 0, time.UTC) }
	return &Tool{Sheets: sh, Prices: prices, Dir: t.TempDir(), Clock: clock, Out: io.Discard}, svc, count
}

// TestRecordedWriteTwice checks against the Sheets API that a second write
// step reuses the spreadsheet the first one made.
func TestRecordedWriteTwice(t *testing.T) {
	tl, _, count := recordedTool(t, "sheets_write_twice")
	ctx := context.Background()
	require.NoError(t, tl.Write(ctx))
	first := stored(t, tl.Dir).SpreadsheetID
	if first == "" {
		t.Fatal("the first Write() stored no spreadsheet")
	}
	require.NoError(t, tl.Write(ctx))
	if count.n != 1 {
		t.Errorf("two write steps made %d spreadsheets, want 1", count.n)
	}
	if got := stored(t, tl.Dir).SpreadsheetID; got != first {
		t.Errorf("the second Write() stored the spreadsheet %q, want the first, %q", got, first)
	}
}

// TestRecordedRead checks against the Sheets API that plain values in the
// values tab read back as they were written, and that the read step uploads
// them. A paste by an administrator is manual, so the test writes the fixture
// into the tab itself.
func TestRecordedRead(t *testing.T) {
	tl, svc, _ := recordedTool(t, "sheets_read_values")
	ctx := context.Background()
	require.NoError(t, tl.Write(ctx))
	id := stored(t, tl.Dir).SpreadsheetID
	// RAW stores each cell as it is given, as a paste of values does.
	require.NoError(t, tl.Sheets.write(ctx, id, valuesTab, "RAW", pasted(t)))

	cells, err := tl.Sheets.read(ctx, id, valuesTab, formula)
	require.NoError(t, err)
	if diff := cmp.Diff(pasted(t), cells); diff != "" {
		t.Errorf("the values tab reads back changed (-written +read):\n%s", diff)
	}
	// One symbol of the fixture is unknown to Google, so the step ends with
	// an error after it has uploaded the others.
	if err := tl.Read(ctx); err == nil {
		t.Error("Read() error = nil, want the skipped series reported")
	}
	if len(svc.uploads) != 1 {
		t.Fatalf("Read() made %d uploads, want 1", len(svc.uploads))
	}
	var symbols []string
	for _, s := range svc.uploads[0].GetSeries() {
		symbols = append(symbols, s.GetSymbol())
	}
	if diff := cmp.Diff([]string{"NASDAQ:AMZN", "NYSE:IBM", "LON:VOD", "CURRENCY:GBPUSD"}, symbols); diff != "" {
		t.Errorf("uploaded series mismatch (-want +got):\n%s", diff)
	}
	if got := svc.uploads[0].GetAsAt(); got != "2026-11-26" {
		t.Errorf("uploaded as at %q, want 2026-11-26", got)
	}
}
