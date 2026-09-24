package datasource

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// failure is an error carrying how the fake classifies it.
type failure struct {
	Failure
	text string
}

func (f failure) Error() string { return f.text }

func temporary(text string) error {
	return failure{Failure: Failure{Temporary: true, Scope: gen.BlockScopeIdentifier, Reason: text}, text: text}
}

func permanent(text string) error {
	return failure{Failure: Failure{Scope: gen.BlockScopeIdentifier, Reason: text}, text: text}
}

func quota(text string) error {
	return failure{Failure: Failure{Temporary: true, Scope: gen.BlockScopeDatasource, Reason: text}, text: text}
}

func credential(text string) error {
	return failure{Failure: Failure{Scope: gen.BlockScopeDatasource, Reason: text}, text: text}
}

// fake is an integration whose every answer is scripted. errs is consumed one
// entry per request, so a case can fail twice and then succeed.
type fake struct {
	// serves reports what to send for a key; a key absent from it is not
	// served.
	serves map[string]types.Identifier
	// errs is the error each successive request fails with, nil to succeed.
	errs []error
	// perKey is the error returned for one position of a good response.
	perKey map[string]error
	// batch is the most keys one request carries.
	batch int

	calls int
	sent  [][]types.Identifier
}

func (f *fake) Serves(key StatedKey) (types.Identifier, error) {
	id, ok := f.serves[key.ID.String()]
	if !ok {
		return types.Identifier{}, errors.New("serves no identifier of this key")
	}
	return id, nil
}

func (f *fake) Fetch(_ context.Context, reqs []FetchRequest[StatedKey]) ([]FetchResponse[IdentityResult], error) {
	f.calls++
	sent := make([]types.Identifier, len(reqs))
	for i, r := range reqs {
		sent[i] = r.Sent
	}
	f.sent = append(f.sent, sent)
	if f.calls <= len(f.errs) {
		if err := f.errs[f.calls-1]; err != nil {
			return nil, err
		}
	}
	out := make([]FetchResponse[IdentityResult], len(sent))
	for i, id := range sent {
		if err, ok := f.perKey[id.Value]; ok {
			out[i] = FetchResponse[IdentityResult]{Err: err}
			continue
		}
		out[i] = FetchResponse[IdentityResult]{Value: IdentityResult{
			Filtered:   []types.Identifier{id},
			Candidates: []Candidate{{Identifiers: []types.Identifier{id}, Class: gen.AssetClassStock, Currency: "USD"}},
		}}
	}
	return out, nil
}

func (f *fake) Classify(err error) Failure {
	var c failure
	if errors.As(err, &c) {
		return c.Failure
	}
	return Failure{Temporary: true, Scope: gen.BlockScopeIdentifier, Reason: err.Error()}
}

func (*fake) Limit() (rate.Limit, int) { return rate.Inf, 1 }

func (f *fake) Batch() int { return f.batch }

func factoryOf(i Integration) Factory {
	return func(Config) (Integration, error) { return i, nil }
}

func failingFactory(msg string) Factory {
	return func(Config) (Integration, error) { return nil, fmt.Errorf("%s", msg) }
}
