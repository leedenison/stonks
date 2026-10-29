package holding

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// covered is a calendar that covers every day to today and holds acts.
func covered(acts ...corpact.Act) corpact.Calendar {
	return corpact.Calendar{Market: "US", Answer: corpact.Answer{Datasource: "alpha", Coverage: []types.Period{{From: date(2000, 1, 1), Before: types.Day(now).AddDate(0, 0, 1)}}, Acts: acts}}
}

func split(ex time.Time, from, to int64) corpact.Act {
	return corpact.Act{ExDate: ex, Ratio: types.Ratio{From: decimal.NewFromInt(from), To: decimal.NewFromInt(to)}, Handled: true}
}

// held is one key with one transaction per day, with the quantities in the
// order given.
func held(days []time.Time, quantities ...string) keyDays {
	k := keyDays{key: gen.StatedKey{ID: uuid.New()}}
	for i, q := range quantities {
		d := decimal.RequireFromString(q)
		k.days, k.raw = append(k.days, day{asAt: days[i], quantity: d}), append(k.raw, d)
	}
	return k
}

// text is a sum in a form that cmp can compare.
type text struct {
	Quantity        string
	Keys            []string
	Mark            holdingv1.HoldingMark
	ProvisionalFrom string
	Gaps            []string
}

func textOf(s sum) text {
	out := text{Quantity: s.quantity.String(), Mark: s.mark}
	for _, k := range s.keys {
		out.Keys = append(out.Keys, k.String())
	}
	if s.provisionalFrom != nil {
		out.ProvisionalFrom = s.provisionalFrom.Format(time.DateOnly)
	}
	for _, g := range s.gaps {
		out.Gaps = append(out.Gaps, g.From.Format(time.DateOnly)+" "+g.Before.Format(time.DateOnly))
	}
	return out
}

func TestRestate(t *testing.T) {
	today := types.Day(now)
	bought, sold := date(2019, 1, 2), date(2021, 3, 1)
	twoForOne := split(date(2020, 3, 2), 1, 2)
	adjusted, provisional := holdingv1.HoldingMark_HOLDING_MARK_ADJUSTED, holdingv1.HoldingMark_HOLDING_MARK_CORP_ACTS_PROVISIONAL
	tests := []struct {
		name string
		cal  corpact.Calendar
		keys []keyDays
		want text
	}{
		{
			name: "a split between the transactions",
			cal:  covered(twoForOne),
			keys: []keyDays{held([]time.Time{bought, sold}, "100", "-50")},
			want: text{Quantity: "150", Keys: []string{"150"}, Mark: adjusted},
		},
		{
			name: "closed across a split",
			cal:  covered(twoForOne),
			keys: []keyDays{held([]time.Time{bought, sold}, "100", "-200")},
			want: text{Quantity: "0", Keys: []string{"0"}, Mark: adjusted},
		},
		{
			name: "a fraction sold for cash cancels at the finest scale stated",
			cal:  covered(split(date(2020, 3, 2), 3, 1)),
			keys: []keyDays{held([]time.Time{bought, sold}, "1", "-0.3333")},
			want: text{Quantity: "0", Keys: []string{"0"}, Mark: adjusted},
		},
		{
			name: "a sum rounded half away from zero",
			cal:  covered(split(date(2020, 3, 2), 2, 3)),
			keys: []keyDays{held([]time.Time{bought}, "1.5")},
			want: text{Quantity: "2.3", Keys: []string{"2.3"}, Mark: adjusted},
		},
		{
			name: "each key rounded on its own",
			cal:  covered(split(date(2020, 3, 2), 3, 1)),
			keys: []keyDays{held([]time.Time{bought}, "1.00"), held([]time.Time{bought}, "1.00")},
			want: text{Quantity: "0.67", Keys: []string{"0.33", "0.33"}, Mark: adjusted},
		},
		{
			name: "as at today with no calendar",
			keys: []keyDays{held([]time.Time{today}, "10")},
			want: text{Quantity: "10", Keys: []string{"10"}, Mark: adjusted},
		},
		{
			name: "no calendar",
			keys: []keyDays{held([]time.Time{bought, sold}, "100", "-200")},
			want: text{Quantity: "-100", Keys: []string{"-100"}, Mark: provisional, ProvisionalFrom: "2019-01-03", Gaps: []string{"2019-01-03 2026-01-11"}},
		},
		{
			name: "a handled split with a gap in one transaction's span",
			cal: corpact.Calendar{Market: "US", Answer: corpact.Answer{
				Datasource: "alpha", Coverage: []types.Period{{From: date(2020, 1, 1), Before: today.AddDate(0, 0, 1)}}, Acts: []corpact.Act{twoForOne},
			}},
			keys: []keyDays{held([]time.Time{bought, sold}, "100", "-50")},
			want: text{Quantity: "150", Keys: []string{"150"}, Mark: provisional, ProvisionalFrom: "2019-01-03", Gaps: []string{"2019-01-03 2020-01-01"}},
		},
		{
			name: "a gap in the middle of the span",
			cal: corpact.Calendar{Market: "US", Answer: corpact.Answer{Datasource: "alpha", Coverage: []types.Period{
				{From: date(2000, 1, 1), Before: date(2020, 1, 1)}, {From: date(2020, 1, 2), Before: today.AddDate(0, 0, 1)},
			}, Acts: []corpact.Act{twoForOne}}},
			keys: []keyDays{held([]time.Time{bought}, "100")},
			want: text{Quantity: "200", Keys: []string{"200"}, Mark: provisional, ProvisionalFrom: "2019-01-03", Gaps: []string{"2020-01-01 2020-01-02"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, textOf(restate(tc.cal, tc.keys, today))); diff != "" {
				t.Errorf("restate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRawSum(t *testing.T) {
	got := textOf(rawSum([]keyDays{held([]time.Time{date(2019, 1, 2), date(2021, 3, 1)}, "100", "-200")}))
	want := text{Quantity: "-100", Keys: []string{"-100"}, Mark: holdingv1.HoldingMark_HOLDING_MARK_RAW}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rawSum mismatch (-want +got):\n%s", diff)
	}
}
