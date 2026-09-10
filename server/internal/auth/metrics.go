package auth

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/leedenison/stonks/server/internal/auth/google"
)

// meterName scopes the instruments below to this package.
const meterName = "github.com/leedenison/stonks/server/internal/auth"

// The attribute keys and the closed sets of values they take. A value outside
// a set is a new time series, so each is a constant rather than a string
// written at the call site.
const (
	outcomeKey = attribute.Key("outcome")
	reasonKey  = attribute.Key("reason")
	resultKey  = attribute.Key("result")

	outcomeSuccess      = "success"
	outcomeInvalidToken = "invalid_token"
	outcomeNotAllowed   = "not_allowed"
	outcomeError        = "error"

	reasonSubject  = "subject"
	reasonEmail    = "email"
	reasonCreated  = "created"
	reasonSignOut  = "sign_out"
	reasonUserGone = "user_gone"

	resultLive     = "live"
	resultNotFound = "not_found"
	resultUserGone = "user_gone"
	resultError    = "error"
)

// instruments count what this package decides. They are built from the global
// meter provider when the package initialises: the API rebinds an instrument
// created before a provider is installed, so nothing here depends on running
// after telemetry setup, and a failed construction yields an instrument that
// does nothing, so no call site is guarded.
type instruments struct {
	signIns     metric.Int64Counter
	provisioned metric.Int64Counter
	creations   metric.Int64Counter
	deletions   metric.Int64Counter
	lookups     metric.Int64Counter
}

var instr = newInstruments(otel.Meter(meterName))

func newInstruments(m metric.Meter) instruments {
	var errs []error
	counter := func(name, unit, desc string) metric.Int64Counter {
		c, err := m.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
		errs = append(errs, err)
		return c
	}
	i := instruments{
		signIns:     counter("stonks.auth.sign_ins", "{sign_in}", "Sign-in attempts by outcome."),
		provisioned: counter("stonks.auth.users_provisioned", "{user}", "Users provisioned at sign-in, by how the account was reached."),
		creations:   counter("stonks.session.creations", "{session}", "Sessions started."),
		deletions:   counter("stonks.session.deletions", "{session}", "Sessions ended, by why."),
		lookups:     counter("stonks.session.lookups", "{lookup}", "Session lookups on an authenticated request, by result."),
	}
	if err := errors.Join(errs...); err != nil {
		otel.Handle(err)
	}
	return i
}

func (i instruments) signIn(ctx context.Context, outcome string) {
	i.signIns.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcome)))
}

func (i instruments) provision(ctx context.Context, reason string) {
	i.provisioned.Add(ctx, 1, metric.WithAttributes(reasonKey.String(reason)))
}

func (i instruments) created(ctx context.Context) { i.creations.Add(ctx, 1) }

func (i instruments) deleted(ctx context.Context, reason string) {
	i.deletions.Add(ctx, 1, metric.WithAttributes(reasonKey.String(reason)))
}

func (i instruments) lookup(ctx context.Context, result string) {
	i.lookups.Add(ctx, 1, metric.WithAttributes(resultKey.String(result)))
}

// signInOutcome classifies a sign-in failure. The set is closed, so a failure
// it does not recognise is an error rather than a new series.
func signInOutcome(err error) string {
	switch {
	case err == nil:
		return outcomeSuccess
	case errors.Is(err, google.ErrMalformed),
		errors.Is(err, google.ErrInvalid),
		errors.Is(err, google.ErrEmailUnverified):
		return outcomeInvalidToken
	case errors.Is(err, ErrNotAllowed):
		return outcomeNotAllowed
	default:
		return outcomeError
	}
}
