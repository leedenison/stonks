package corpact

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

var today = day(2026, 1, 10)

// canned is a set of rows that the calendar queries return.
type canned struct {
	markets  map[uuid.UUID]string
	naming   []gen.ListTickersNamingRow
	validity []gen.ListTickerValidityRow
	answers  []gen.ListCorpActAnswersRow
	acts     []gen.CorpAct
}

func (s *canned) ListInstrMarkets(context.Context, []uuid.UUID) ([]gen.ListInstrMarketsRow, error) {
	var out []gen.ListInstrMarketsRow
	for id, m := range s.markets {
		out = append(out, gen.ListInstrMarketsRow{InstrID: id, Market: m})
	}
	return out, nil
}

func (s *canned) ListTickersNaming(context.Context, []uuid.UUID) ([]gen.ListTickersNamingRow, error) {
	return s.naming, nil
}

func (s *canned) ListTickerValidity(context.Context, gen.ListTickerValidityParams) ([]gen.ListTickerValidityRow, error) {
	return s.validity, nil
}

func (s *canned) ListCorpActAnswers(context.Context, gen.ListCorpActAnswersParams) ([]gen.ListCorpActAnswersRow, error) {
	return s.answers, nil
}

func (s *canned) ListCorpActs(context.Context, gen.ListCorpActsParams) ([]gen.CorpAct, error) {
	return s.acts, nil
}

// at makes market the market of instr.
func (s *canned) at(instr uuid.UUID, market string) {
	if s.markets == nil {
		s.markets = map[uuid.UUID]string{}
	}
	s.markets[instr] = market
}

// names records that market:value names instr from the day start until the
// day end. A nil bound is open. The first market that names instr becomes its
// market.
func (s *canned) names(market, value string, instr uuid.UUID, start, end *time.Time) {
	if _, ok := s.markets[instr]; !ok {
		s.at(instr, market)
	}
	s.naming = append(s.naming, gen.ListTickersNamingRow{InstrID: instr, Market: market, Value: value})
	s.validity = append(s.validity, gen.ListTickerValidityRow{
		Market: market, Value: value, InstrID: instr, ValidFrom: start, ValidBefore: end, Validity: gen.ValidityConfirmed,
	})
}

// answer records the answer of datasource for market:value, covering from
// up to before, and returns its id.
func (s *canned) answer(datasource, market, value string, from, before time.Time) uuid.UUID {
	id := uuid.New()
	s.answers = append(s.answers, gen.ListCorpActAnswersRow{
		CorpActAnswer: gen.CorpActAnswer{ID: id, Datasource: datasource, CoveredFrom: &from, CoveredBefore: &before},
		Market:        market, Value: value,
	})
	return id
}

// act records an action of answer, from units becoming to units.
func (s *canned) act(answer uuid.UUID, ex time.Time, from, to int64, handled bool) {
	s.acts = append(s.acts, gen.CorpAct{
		ID: uuid.New(), AnswerID: answer, ExDate: ex, RatioFrom: decimal.NewFromInt(from), RatioTo: decimal.NewFromInt(to), Handled: handled,
	})
}

func (s *canned) calendars(t *testing.T, instrs ...uuid.UUID) map[uuid.UUID]Calendar {
	t.Helper()
	cals, err := Calendars(context.Background(), s, instrs)
	require.NoError(t, err)
	return cals
}

func split(ex time.Time, from, to int64, handled bool) Act {
	return Act{ExDate: ex, Ratio: types.Ratio{From: decimal.NewFromInt(from), To: decimal.NewFromInt(to)}, Handled: handled}
}

func period(from, before time.Time) types.Period { return types.Period{From: from, Before: before} }

var ignoreActID = cmpopts.IgnoreFields(Act{}, "ID")

// TestCalendarsRename checks that the coverage of two tickers that named an
// instrument one after the other joins into one period.
func TestCalendarsRename(t *testing.T) {
	instr := uuid.New()
	var s canned
	renamed := day(2022, 6, 9)
	s.names("US", "FB", instr, nil, &renamed)
	s.names("US", "META", instr, &renamed, nil)
	s.answer("alpha", "US", "FB", day(2012, 1, 1), today.AddDate(0, 0, 1))
	s.answer("alpha", "US", "META", day(2012, 1, 1), today.AddDate(0, 0, 1))
	got := s.calendars(t, instr)[instr]
	want := Calendar{Market: "US", Answer: Answer{Datasource: "alpha", Coverage: []types.Period{period(day(2012, 1, 1), today.AddDate(0, 0, 1))}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("calendar mismatch (-want +got):\n%s", diff)
	}
}

// TestCalendarsTwice checks that the calendar keeps an action once when two
// tickers of one market report it.
func TestCalendarsTwice(t *testing.T) {
	instr := uuid.New()
	var s canned
	s.names("US", "BRK.B", instr, nil, nil)
	s.names("US", "BRK/B", instr, nil, nil)
	for _, v := range []string{"BRK.B", "BRK/B"} {
		a := s.answer("alpha", "US", v, day(2012, 1, 1), today.AddDate(0, 0, 1))
		s.act(a, day(2020, 3, 2), 1, 2, true)
	}
	got := s.calendars(t, instr)[instr]
	if diff := cmp.Diff([]Act{split(day(2020, 3, 2), 1, 2, true)}, got.Acts, ignoreActID); diff != "" {
		t.Errorf("acts mismatch (-want +got):\n%s", diff)
	}
	if f, ok := got.Factor(day(2019, 1, 1), today); !ok || f.Cmp(big.NewRat(2, 1)) != 0 {
		t.Errorf("Factor = %v, %v, want 2, true", f, ok)
	}
}

// TestCalendarsHolder checks that an action and coverage go to the
// instrument that the ticker named on their day.
func TestCalendarsHolder(t *testing.T) {
	first, later := uuid.New(), uuid.New()
	var s canned
	passed := day(2015, 1, 1)
	s.names("US", "ABC", first, nil, &passed)
	s.names("US", "ABC", later, &passed, nil)
	a := s.answer("alpha", "US", "ABC", day(2010, 1, 1), today.AddDate(0, 0, 1))
	s.act(a, day(2014, 5, 1), 1, 2, true)
	cals := s.calendars(t, first, later)
	if diff := cmp.Diff([]Act{split(day(2014, 5, 1), 1, 2, true)}, cals[first].Acts, ignoreActID); diff != "" {
		t.Errorf("acts of the first holder mismatch (-want +got):\n%s", diff)
	}
	if got := cals[later]; len(got.Acts) != 0 || !cmp.Equal(got.Coverage, []types.Period{period(passed, today.AddDate(0, 0, 1))}) {
		t.Errorf("later holder = %+v, want coverage from %s and no acts", got, passed.Format(time.DateOnly))
	}
}

// TestCalendarsUnhandled checks that an unhandled action takes its day out
// of the coverage.
func TestCalendarsUnhandled(t *testing.T) {
	instr := uuid.New()
	var s canned
	s.names("US", "GE", instr, nil, nil)
	spun := day(2024, 4, 2)
	us := s.answer("alpha", "US", "GE", day(2012, 1, 1), today.AddDate(0, 0, 1))
	s.act(us, spun, 1000, 1253, false)
	cal := s.calendars(t, instr)[instr]
	if _, ok := cal.Factor(day(2023, 6, 1), today); ok {
		t.Error("Factor covered a span across the unhandled action")
	}
	want := []types.Period{period(spun, spun.AddDate(0, 0, 1))}
	if diff := cmp.Diff(want, cal.Gaps(period(day(2023, 6, 2), today.AddDate(0, 0, 1)))); diff != "" {
		t.Errorf("gaps mismatch (-want +got):\n%s", diff)
	}
	if f, ok := cal.Factor(spun, today); !ok || f.Cmp(big.NewRat(1, 1)) != 0 {
		t.Errorf("Factor after the action = %v, %v, want 1, true", f, ok)
	}
}

// TestCalendarsOneMarket checks that a calendar holds only the answer of the
// instrument's market, and that an instrument without a market has none.
func TestCalendarsOneMarket(t *testing.T) {
	instr, unplaced := uuid.New(), uuid.New()
	var s canned
	s.names("US", "X", instr, nil, nil)
	s.names("XTSE", "X", instr, nil, nil)
	s.naming = append(s.naming, gen.ListTickersNamingRow{InstrID: unplaced, Market: "US", Value: "Y"})
	s.validity = append(s.validity, gen.ListTickerValidityRow{Market: "US", Value: "Y", InstrID: unplaced, Validity: gen.ValidityConfirmed})
	s.answer("alpha", "US", "X", day(2012, 1, 1), today)
	ca := s.answer("beta", "XTSE", "X", day(2012, 1, 1), today)
	s.act(ca, day(2020, 3, 2), 1, 2, true)
	s.answer("alpha", "US", "Y", day(2012, 1, 1), today)
	cals := s.calendars(t, instr, unplaced)
	if got := cals[instr]; got.Market != "US" || got.Datasource != "alpha" || len(got.Acts) != 0 {
		t.Errorf("calendar = %+v, want the US answer of alpha with no acts", got)
	}
	if got, ok := cals[unplaced]; ok {
		t.Errorf("calendar of an instrument without a market = %+v, want none", got)
	}
}

func TestFactor(t *testing.T) {
	whole := []types.Period{period(day(2010, 1, 1), today.AddDate(0, 0, 1))}
	answer := func(cov []types.Period, acts ...Act) Calendar {
		return Calendar{Market: "US", Answer: Answer{Datasource: "alpha", Coverage: cov, Acts: acts}}
	}
	tests := []struct {
		name    string
		cal     Calendar
		asAt    time.Time
		want    *big.Rat
		covered bool
	}{
		{name: "one split", cal: answer(whole, split(day(2020, 3, 2), 1, 4, true)), asAt: day(2019, 1, 1), want: big.NewRat(4, 1), covered: true},
		{name: "a reverse split", cal: answer(whole, split(day(2020, 3, 2), 3, 1, true)), asAt: day(2019, 1, 1), want: big.NewRat(1, 3), covered: true},
		{name: "two splits", cal: answer(whole, split(day(2014, 6, 9), 1, 7, true), split(day(2020, 3, 2), 1, 4, true)), asAt: day(2013, 1, 1), want: big.NewRat(28, 1), covered: true},
		{name: "an ex-date on the as-at day", cal: answer(whole, split(day(2020, 3, 2), 1, 4, true)), asAt: day(2020, 3, 2), want: big.NewRat(1, 1), covered: true},
		{name: "an ex-date today", cal: answer(whole, split(today, 1, 4, true)), asAt: day(2020, 3, 2), want: big.NewRat(4, 1), covered: true},
		{name: "a future ex-date", cal: answer(whole, split(today.AddDate(0, 0, 1), 1, 4, true)), asAt: day(2020, 3, 2), want: big.NewRat(1, 1), covered: true},
		{name: "an unhandled action", cal: answer(whole, split(day(2020, 3, 2), 1000, 1253, false)), asAt: day(2019, 1, 1), want: big.NewRat(1, 1), covered: true},
		{name: "a gap today", cal: answer([]types.Period{period(day(2010, 1, 1), today)}), asAt: day(2019, 1, 1), want: big.NewRat(1, 1)},
		{
			name: "a split known across a gap",
			cal:  answer([]types.Period{period(day(2010, 1, 1), day(2020, 1, 1)), period(day(2020, 1, 2), today.AddDate(0, 0, 1))}, split(day(2020, 3, 2), 1, 2, true)),
			asAt: day(2019, 1, 1), want: big.NewRat(2, 1),
		},
		{name: "as at today with no calendar", asAt: today, want: big.NewRat(1, 1), covered: true},
		{name: "no calendar", asAt: day(2019, 1, 1), want: big.NewRat(1, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.cal.Factor(tc.asAt, today)
			if ok != tc.covered || got.Cmp(tc.want) != 0 {
				t.Errorf("Factor = %v, %v, want %v, %v", got, ok, tc.want, tc.covered)
			}
		})
	}
}

func TestGaps(t *testing.T) {
	p := period(day(2020, 1, 1), day(2021, 1, 1))
	answer := func(cov ...types.Period) Calendar {
		return Calendar{Market: "US", Answer: Answer{Coverage: cov}}
	}
	tests := []struct {
		name string
		cal  Calendar
		want []types.Period
	}{
		{name: "no calendar", want: []types.Period{p}},
		{name: "covered", cal: answer(period(day(2019, 1, 1), day(2022, 1, 1)))},
		{
			name: "covered in two periods that touch",
			cal:  answer(period(day(2019, 1, 1), day(2020, 7, 1)), period(day(2020, 7, 1), day(2022, 1, 1))),
		},
		{
			name: "a day lacking",
			cal:  answer(period(day(2019, 1, 1), day(2020, 7, 1)), period(day(2020, 7, 2), day(2022, 1, 1))),
			want: []types.Period{period(day(2020, 7, 1), day(2020, 7, 2))},
		},
		{
			name: "both ends lacking",
			cal:  answer(period(day(2020, 3, 1), day(2020, 9, 1))),
			want: []types.Period{period(day(2020, 1, 1), day(2020, 3, 1)), period(day(2020, 9, 1), day(2021, 1, 1))},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, tc.cal.Gaps(p)); diff != "" {
				t.Errorf("gaps mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
