package googlesheets

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/market"
)

func TestFactory(t *testing.T) {
	tests := []struct {
		name    string
		cfg     market.Config
		want    string
		wantErr bool
	}{
		{name: "default endpoint", cfg: market.Config{Name: Name}, want: "https://sheets.googleapis.com"},
		{name: "endpoint of the row", cfg: market.Config{Name: Name, Endpoint: "http://sheets.example.com"}, want: "http://sheets.example.com"},
		{name: "endpoint that is not a URL", cfg: market.Config{Name: Name, Endpoint: "sheets"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Factory().New(tc.cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("New(%+v) error = %v, wantErr %v", tc.cfg, err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got.Endpoint() != tc.want {
				t.Errorf("New(%+v).Endpoint() = %q, want %q", tc.cfg, got.Endpoint(), tc.want)
			}
		})
	}
}

// A fetch passes over an integration by the capability it lacks.
func TestServesNoKind(t *testing.T) {
	got, err := Factory().New(market.Config{Name: Name})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := got.(market.Prices); ok {
		t.Error("the integration serves prices")
	}
	if _, ok := got.(market.CorpActs); ok {
		t.Error("the integration serves corporate actions")
	}
	if _, ok := got.(market.IdentifierFacts); ok {
		t.Error("the integration serves identifier facts")
	}
	if _, ok := got.(market.Identity); ok {
		t.Error("the integration serves identity")
	}
}
