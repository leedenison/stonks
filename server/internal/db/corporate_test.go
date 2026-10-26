//go:build dbtest

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// newCorporateEvent records an answer of f's datasource for US:NVDA under a
// served key of f's fetch, with one unhandled event.
func newCorporateEvent(t *testing.T, q *gen.Queries, f fetching) (answer, event uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	answer, event = db.NewID(), db.NewID()
	require.NoError(t, q.CreateCorporateAnswer(ctx, gen.CreateCorporateAnswerParams{
		ID: answer, FetchKeyID: servedKey(t, q, f.fetch, f.key, "US67066G1040"), Datasource: f.ds.Name, Market: "US", Value: "NVDA",
		CoveredFrom: ptr.To(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)), CoveredBefore: ptr.To(time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)),
	}))
	require.NoError(t, q.CreateCorporateEvent(ctx, gen.CreateCorporateEventParams{
		ID: event, AnswerID: answer, ExDate: time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC),
		RatioFrom: decimal.NewFromInt(1), RatioTo: decimal.NewFromInt(10), Type: ptr.To("spin_off"), Handled: false,
	}))
	return answer, event
}

// TestCorporateEvents checks that an answer reads with the moment its fetch
// key states and its datasource's precedence, that its events read by
// ticker and period, and that deleting an event deletes the findings that
// name it.
func TestCorporateEvents(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	f := newFetching(t, q)
	answer, event := newCorporateEvent(t, q, f)

	got, err := q.GetCorporateAnswer(ctx, gen.GetCorporateAnswerParams{Datasource: f.ds.Name, Market: "US", Value: "NVDA"})
	require.NoError(t, err)
	if got.ID != answer || !got.AnsweredAt.Equal(answered) {
		t.Errorf("GetCorporateAnswer = %+v, want %s answered at %s", got, answer, answered)
	}
	answers, err := q.ListCorporateAnswers(ctx, gen.ListCorporateAnswersParams{Markets: []string{"US"}, Values: []string{"NVDA"}})
	require.NoError(t, err)
	if len(answers) != 1 || answers[0].CorporateEventAnswer.ID != answer || answers[0].Precedence != f.ds.Precedence {
		t.Errorf("ListCorporateAnswers = %+v, want the answer with precedence %d", answers, f.ds.Precedence)
	}
	for _, tc := range []struct {
		name         string
		from, before time.Time
		want         int
	}{
		{name: "a period holding the ex-date", from: time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC), before: time.Date(2024, 6, 11, 0, 0, 0, 0, time.UTC), want: 1},
		{name: "a period ending on the ex-date", from: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), before: time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC), want: 0},
	} {
		events, err := q.ListCorporateEvents(ctx, gen.ListCorporateEventsParams{Markets: []string{"US"}, Values: []string{"NVDA"}, FromDay: tc.from, BeforeDay: tc.before})
		require.NoError(t, err)
		if len(events) != tc.want {
			t.Errorf("ListCorporateEvents over %s = %d events, want %d", tc.name, len(events), tc.want)
		}
	}

	finding := gen.CreateFindingParams{ID: db.NewID(), RunID: f.fetch.ID, Kind: gen.FindingKindUnhandledCorporateEvent, CorporateEventID: &event, Detail: ptr.To("US:NVDA on 2024-06-10")}
	require.NoError(t, q.CreateFinding(ctx, finding))
	disagreed, err := q.ListDisagreedEvents(ctx, []uuid.UUID{event})
	require.NoError(t, err)
	if len(disagreed) != 0 {
		t.Errorf("ListDisagreedEvents = %v, want none for an unhandled finding", disagreed)
	}
	require.NoError(t, q.DeleteCorporateEvents(ctx, []uuid.UUID{event}))
	listed, err := q.ListRunFindings(ctx, []uuid.UUID{f.fetch.ID})
	require.NoError(t, err)
	if len(listed) != 0 {
		t.Errorf("findings after deleting the event = %+v, want none", listed)
	}
}

// TestCorporateEventChecks checks the constraints of answers and events.
func TestCorporateEventChecks(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		sql  string
		code string
	}{
		{name: "an answer with no market", sql: "INSERT INTO corporate_event_answers (id, fetch_key_id, datasource, market, value) SELECT $1, $2, datasource, '', 'AAPL' FROM corporate_event_answers WHERE id = $3", code: pgerrcode.CheckViolation},
		{name: "coverage with one bound", sql: "INSERT INTO corporate_event_answers (id, fetch_key_id, datasource, market, value, covered_from) SELECT $1, $2, datasource, 'US', 'AAPL', '2024-01-01' FROM corporate_event_answers WHERE id = $3", code: pgerrcode.CheckViolation},
		{name: "coverage that ends as it starts", sql: "INSERT INTO corporate_event_answers (id, fetch_key_id, datasource, market, value, covered_from, covered_before) SELECT $1, $2, datasource, 'US', 'AAPL', '2024-01-01', '2024-01-01' FROM corporate_event_answers WHERE id = $3", code: pgerrcode.CheckViolation},
		{name: "a second answer of a datasource for a ticker", sql: "INSERT INTO corporate_event_answers (id, fetch_key_id, datasource, market, value) SELECT $1, $2, datasource, market, value FROM corporate_event_answers WHERE id = $3", code: pgerrcode.UniqueViolation},
		{name: "a ratio from zero", sql: "INSERT INTO corporate_events (id, answer_id, ex_date, ratio_from, ratio_to, handled) SELECT $1, $3, '2024-06-10', 0, 10, true WHERE $2::uuid IS NOT NULL", code: pgerrcode.CheckViolation},
		{name: "a ratio to zero", sql: "INSERT INTO corporate_events (id, answer_id, ex_date, ratio_from, ratio_to, handled) SELECT $1, $3, '2024-06-10', 1, 0, true WHERE $2::uuid IS NOT NULL", code: pgerrcode.CheckViolation},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := begin(t)
			q := gen.New(tx)
			f := newFetching(t, q)
			answer, _ := newCorporateEvent(t, q, f)
			key := servedKey(t, q, newFetch(t, q, f.user, f.run, f.ds), f.key, "US0378331005")
			if _, err := tx.Exec(ctx, tc.sql, db.NewID(), key, answer); !sqlstate(err, tc.code) {
				t.Errorf("%s: err = %v, want %s", tc.name, err, tc.code)
			}
		})
	}
}
