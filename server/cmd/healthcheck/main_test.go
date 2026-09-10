package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "ok", status: http.StatusOK},
		{name: "unavailable", status: http.StatusServiceUnavailable, wantErr: true},
		{name: "not found", status: http.StatusNotFound, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)
			err := check(context.Background(), srv.URL+"/healthz")
			if (err != nil) != tc.wantErr {
				t.Errorf("check() with status %d: error = %v, wantErr %v", tc.status, err, tc.wantErr)
			}
		})
	}

	t.Run("unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		if err := check(context.Background(), srv.URL+"/healthz"); err == nil {
			t.Error("check() of a closed server: error = nil, want one")
		}
	})
}
