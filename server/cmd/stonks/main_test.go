package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/mock/gomock"

	instrumentv1 "github.com/leedenison/stonks/proto/instrument/v1"
	"github.com/leedenison/stonks/proto/instrument/v1/instrumentv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/auth/mock"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service"
)

// spans records what the handler chain traced. The tracer provider installs
// once per process, so one recorder serves every case and each resets it.
var spans = tracetest.NewSpanRecorder()

func TestMain(m *testing.M) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	os.Exit(m.Run())
}

// endedNames returns the names of the spans recorded since the last reset.
func endedNames() []string {
	ended := spans.Ended()
	names := make([]string, 0, len(ended))
	for _, s := range ended {
		names = append(names, s.Name())
	}
	return names
}

// withCookie sends the session cookie with every request.
type withCookie struct {
	next http.RoundTripper
}

func (w withCookie) RoundTrip(req *http.Request) (*http.Response, error) {
	req.AddCookie(&http.Cookie{Name: service.CookieName, Value: "live"})
	return w.next.RoundTrip(req)
}

func TestServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	userID := uuid.New()
	sessions := mock.NewMockSessionStore(ctrl)
	sessions.EXPECT().Get(gomock.Any(), "live").Return(session.Session{ID: "live", UserID: userID, ExpiresAt: time.Now().Add(time.Hour)}, nil).AnyTimes()
	sessions.EXPECT().Get(gomock.Any(), gomock.Any()).Return(session.Session{}, session.ErrNotFound).AnyTimes()
	users := mock.NewMockUserStore(ctrl)
	users.EXPECT().GetUser(gomock.Any(), userID).Return(gen.User{ID: userID, Email: "one@example.com", Role: gen.UserRoleUser}, nil).AnyTimes()
	authn := auth.New(auth.Options{Users: users, Sessions: sessions})

	srv := httptest.NewUnstartedServer(nil)
	cfg, err := newServer("", slog.New(slog.DiscardHandler), authn, true)
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	srv.Config = cfg
	srv.Start()
	t.Cleanup(srv.Close)

	spans.Reset()
	res, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if err := res.Body.Close(); err != nil {
		t.Error(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	// The container probes this every two seconds, so it is deliberately
	// outside the Connect chain and the mux carries no HTTP instrumentation.
	// Wrapping the mux would fail here.
	if names := endedNames(); len(names) != 0 {
		t.Errorf("GET /healthz recorded spans %v, want none", names)
	}

	// A transport allowing only unencrypted HTTP/2 speaks it with prior
	// knowledge, which the server accepts only when it is enabled.
	h2Only := new(http.Protocols)
	h2Only.SetUnencryptedHTTP2(true)
	h2Plain := &http.Client{Transport: &http.Transport{Protocols: h2Only}}
	h1 := &http.Client{Transport: withCookie{srv.Client().Transport}}
	h2 := &http.Client{Transport: withCookie{h2Plain.Transport}}
	tests := []struct {
		name   string
		client instrumentv1connect.InstrumentServiceClient
	}{
		{name: "connect over http/1.1", client: instrumentv1connect.NewInstrumentServiceClient(h1, srv.URL)},
		{name: "grpc over unencrypted http/2", client: instrumentv1connect.NewInstrumentServiceClient(h2, srv.URL, connect.WithGRPC())},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spans.Reset()
			_, err := tc.client.ListInstruments(context.Background(), connect.NewRequest(&instrumentv1.ListInstrumentsRequest{}))
			if err != nil {
				t.Errorf("ListInstruments: %v", err)
			}
			// The span is named for the procedure without its leading slash.
			want := []string{strings.TrimPrefix(instrumentv1connect.InstrumentServiceListInstrumentsProcedure, "/")}
			if got := endedNames(); !slices.Equal(got, want) {
				t.Errorf("ListInstruments recorded spans %v, want %v", got, want)
			}
		})
	}

	t.Run("no session", func(t *testing.T) {
		client := instrumentv1connect.NewInstrumentServiceClient(srv.Client(), srv.URL)
		_, err := client.ListInstruments(context.Background(), connect.NewRequest(&instrumentv1.ListInstrumentsRequest{}))
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("ListInstruments without a session: code = %v (err %v), want %v", connect.CodeOf(err), err, connect.CodeUnauthenticated)
		}
	})

	t.Run("reflection", func(t *testing.T) {
		spans.Reset()
		client := grpcreflect.NewClient(h2Plain, srv.URL)
		stream := client.NewStream(context.Background())
		t.Cleanup(func() {
			if _, err := stream.Close(); err != nil {
				t.Error(err)
			}
		})
		names, err := stream.ListServices()
		if err != nil {
			t.Fatalf("ListServices: %v", err)
		}
		if len(names) != 2 {
			t.Errorf("ListServices = %v, want the two services", names)
		}
		if got := endedNames(); len(got) != 0 {
			t.Errorf("reflection recorded spans %v, want none", got)
		}
	})
}
