package mic

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

func TestOperating(t *testing.T) {
	tbl := Of(map[string]string{"XNAS": "XNAS", "XNGS": "XNAS"})
	tests := []struct {
		name string
		mic  string
		want string
		ok   bool
	}{
		{name: "operating", mic: "XNAS", want: "XNAS", ok: true},
		{name: "segment", mic: "XNGS", want: "XNAS", ok: true},
		{name: "case and space", mic: " xngs ", want: "XNAS", ok: true},
		{name: "unknown", mic: "ZZZZ"},
		{name: "empty", mic: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tbl.Operating(tc.mic)
			if got != tc.want || ok != tc.ok {
				t.Errorf("Operating(%q) = %q, %v, want %q, %v", tc.mic, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// us is a map in which XNAS and XNYS share the US market and XLON is a
// market of its own.
var us = MICMap{
	"XNAS": {Operating: "XNAS", Market: "US"},
	"XNGS": {Operating: "XNAS", Market: "US"},
	"XNYS": {Operating: "XNYS", Market: "US"},
	"ARCX": {Operating: "XNYS", Market: "US"},
	"XLON": {Operating: "XLON", Market: "XLON"},
}

func TestMarket(t *testing.T) {
	tests := []struct {
		name string
		mic  string
		want string
		ok   bool
	}{
		{name: "shared", mic: "XNYS", want: "US", ok: true},
		{name: "segment", mic: " xngs ", want: "US", ok: true},
		{name: "own", mic: "XLON", want: "XLON", ok: true},
		{name: "market code", mic: "US"},
		{name: "unknown", mic: "ZZZZ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := us.Market(tc.mic)
			if got != tc.want || ok != tc.ok {
				t.Errorf("Market(%q) = %q, %v, want %q, %v", tc.mic, got, ok, tc.want, tc.ok)
			}
		})
	}
	if !us.IsMarket("US") || !us.IsMarket("XLON") || us.IsMarket("XNAS") {
		t.Errorf("IsMarket: want US and XLON markets, and XNAS none")
	}
}

func TestMarketTicker(t *testing.T) {
	ticker := func(d, v string) types.Identifier {
		return types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: d, Value: v}
	}
	tests := []struct {
		name string
		v    types.Identifier
		want types.MarketTicker
		ok   bool
	}{
		{name: "segment", v: ticker("ARCX", "SPY"), want: types.MarketTicker{Market: "US", Value: "SPY"}, ok: true},
		{name: "own market", v: ticker("XLON", "VOD"), want: types.MarketTicker{Market: "XLON", Value: "VOD"}, ok: true},
		{name: "bare", v: ticker("", "SPY")},
		{name: "domain", v: ticker("XNAS", "")},
		{name: "unknown", v: ticker("ZZZZ", "SPY")},
		{name: "other type", v: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000000"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := us.MarketTicker(tc.v)
			if got != tc.want || ok != tc.ok {
				t.Errorf("MarketTicker(%v) = %v, %v, want %v, %v", tc.v, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestVenues(t *testing.T) {
	if got, want := us.Venues("US"), []string{"ARCX", "XNAS", "XNGS", "XNYS"}; !slices.Equal(got, want) {
		t.Errorf("Venues(US) = %v, want %v", got, want)
	}
	if got := us.Venues("XNAS"); len(got) != 0 {
		t.Errorf("Venues(XNAS) = %v, want none, since XNAS is in the US market", got)
	}
}

func TestTickerProbes(t *testing.T) {
	got, err := us.TickerProbes([]types.MarketTicker{{Market: "US", Value: "FOO"}, {Market: "XLON", Value: "VOD"}})
	require.NoError(t, err)
	want := []string{
		`[{"domain":"ARCX","type":"mic_ticker","value":"FOO"}]`,
		`[{"domain":"XNAS","type":"mic_ticker","value":"FOO"}]`,
		`[{"domain":"XNGS","type":"mic_ticker","value":"FOO"}]`,
		`[{"domain":"XNYS","type":"mic_ticker","value":"FOO"}]`,
		`[{"domain":"XLON","type":"mic_ticker","value":"VOD"}]`,
	}
	if diff := cmp.Diff(want, strs(got)); diff != "" {
		t.Errorf("TickerProbes mismatch (-want +got):\n%s", diff)
	}
	if _, err := us.TickerProbes([]types.MarketTicker{{Market: "XNAS", Value: "FOO"}}); err == nil {
		t.Error("TickerProbes(XNAS:FOO) succeeded, want an error, since XNAS is no market")
	}
}

// stub is the reference table Load reads.
type stub []gen.MicMarket

func (s stub) ListMICs(context.Context) ([]gen.MicMarket, error) { return s, nil }

func TestLoad(t *testing.T) {
	got, err := Load(context.Background(), stub{
		{Mic: "XNGS", OperatingMic: "XNAS", Market: "US"},
		{Mic: "XLON", OperatingMic: "XLON", Market: "XLON"},
	})
	require.NoError(t, err)
	want := MICMap{"XNGS": {Operating: "XNAS", Market: "US"}, "XLON": {Operating: "XLON", Market: "XLON"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Load mismatch (-want +got):\n%s", diff)
	}
}

func strs(bs [][]byte) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = string(b)
	}
	return out
}
