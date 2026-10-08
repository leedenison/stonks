package market

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/run"
)

// Result is what one request of a fetch got.
type Result[Q, P any] struct {
	Request Q
	// Source is the datasource that served the request.
	Source string
	// ID is the fetch_keys row, where the consumer attaches its response.
	ID      uuid.UUID
	Outcome gen.FetchOutcome
	// Sent is the identifier the request went out under, nil where the
	// datasource does not serve the request.
	Sent     *types.Identifier
	Reason   string
	Response P

	// attempts is the number of calls that carried the request.
	attempts int
}

// Fetcher runs fetches against the datasources of a registry.
type Fetcher struct {
	store Store
	runs  Runner
	log   *slog.Logger

	base  time.Duration
	tries int
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// Option adjusts a Fetcher.
type Option func(*Fetcher)

// WithRetry sets the first wait after a temporary failure and the number of
// calls one request may cost.
func WithRetry(base time.Duration, tries int) Option {
	return func(f *Fetcher) { f.base, f.tries = base, tries }
}

// WithClock sets the clock and the sleep a Fetcher uses. The sleep returns
// the context's error once ctx is done.
func WithClock(now func() time.Time, sleep func(ctx context.Context, d time.Duration) error) Option {
	return func(f *Fetcher) { f.now, f.sleep = now, sleep }
}

// NewFetcher returns a Fetcher over store.
func NewFetcher(store Store, runs Runner, log *slog.Logger, opts ...Option) *Fetcher {
	f := &Fetcher{store: store, runs: runs, log: log, base: retryBase, tries: retryTries, now: time.Now, sleep: sleep}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Fetch requests kind about reqs from e, as a child run of parent. It returns
// one result per request, in the order they were given.
func Fetch[Q, P any](ctx context.Context, f *Fetcher, parent gen.Run, e *Entry, kind Kind[Q, P], reqs []Q) ([]Result[Q, P], error) {
	var results []Result[Q, P]
	_, err := f.runs.Child(ctx, parent, gen.RunKindFetch, func(ctx context.Context, run gen.Run) error {
		var werr error
		results, werr = fetch(ctx, f, run, e, kind, reqs)
		return werr
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func fetch[Q, P any](ctx context.Context, f *Fetcher, row gen.Run, e *Entry, kind Kind[Q, P], reqs []Q) ([]Result[Q, P], error) {
	if _, err := f.store.CreateFetch(ctx, gen.CreateFetchParams{
		ID: row.ID, UserID: row.UserID, Datasource: e.Name, Endpoint: e.Integration.Endpoint(), Kind: kind.name,
	}); err != nil {
		return nil, fmt.Errorf("create fetch: %w", err)
	}
	open, err := f.store.ListOpenBlocks(ctx, gen.ListOpenBlocksParams{Datasource: e.Name, Kind: kind.name})
	if err != nil {
		return nil, fmt.Errorf("list open blocks: %w", err)
	}
	blocked := newBlockSet(open)

	s := kind.server(e)
	results := make([]Result[Q, P], len(reqs))
	var batch []int
	for i, q := range reqs {
		results[i] = Result[Q, P]{Request: q, Source: e.Name, ID: db.NewID()}
		if s == nil {
			results[i].Outcome, results[i].Reason = gen.FetchOutcomeNotServed, fmt.Sprintf("the datasource serves no %s", kind.name)
			continue
		}
		sent, err := s.Serves(q)
		if err != nil {
			results[i].Outcome, results[i].Reason = gen.FetchOutcomeNotServed, err.Error()
			continue
		}
		results[i].Sent = &sent
		if reason, ok := blocked.covers(sent); ok {
			results[i].Outcome, results[i].Reason = gen.FetchOutcomeBlocked, reason
			continue
		}
		batch = append(batch, i)
	}

	// A request that blocks an identifier or the datasource suppresses the
	// requests after it, as an open block does.
	var pending []gen.CreateDatasourceBlockParams
	for _, chunk := range chunks(batch, e.Integration.Batch()) {
		var send []int
		for _, i := range chunk {
			if reason, ok := blocked.covers(*results[i].Sent); ok {
				results[i].Outcome, results[i].Reason = gen.FetchOutcomeBlocked, reason
				continue
			}
			send = append(send, i)
		}
		if len(send) == 0 {
			continue
		}
		blocks := fetchChunk(ctx, f, e, s, results, send)
		blocked.add(blocks)
		pending = append(pending, blocks...)
	}
	found := 0
	err = f.store.Tx(ctx, func(q Queries) error {
		for i := range results {
			if err := record(ctx, q, row, kind, results[i]); err != nil {
				return err
			}
		}
		for _, b := range pending {
			b.ID, b.Datasource, b.Kind = db.NewID(), e.Name, kind.name
			b.FindingID, b.RunID = db.NewID(), row.ID
			n, err := q.CreateDatasourceBlock(ctx, b)
			if err != nil {
				return fmt.Errorf("create block: %w", err)
			}
			found += int(n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		instr.key(ctx, e.Name, r.Outcome)
	}
	for range found {
		run.Found(ctx, gen.FindingKindBlock)
	}
	return results, nil
}

// fetchChunk sends the requests of batch and fills their results. It returns the
// blocks its failures require, which are written once their fetch keys exist.
func fetchChunk[Q, P any](ctx context.Context, f *Fetcher, e *Entry, s Server[Q, P], results []Result[Q, P], batch []int) []gen.CreateDatasourceBlockParams {
	reqs := make([]Request[Q], len(batch))
	for n, i := range batch {
		reqs[n] = Request[Q]{Value: results[i].Request, Sent: *results[i].Sent}
	}
	resps, attempts, err := request(ctx, f, e, s, reqs)
	if err != nil {
		return requestFailed(e.Integration.Classify(err), err, results, batch, attempts)
	}

	var blocks []gen.CreateDatasourceBlockParams
	for n, i := range batch {
		r := &results[i]
		r.attempts = attempts
		if n >= len(resps) {
			r.Outcome, r.Reason = gen.FetchOutcomeFailedTemporary, "the datasource returned fewer results than it was sent"
			continue
		}
		resp := resps[n]
		if resp.Err == nil {
			r.Outcome, r.Response = gen.FetchOutcomeServed, resp.Value
			continue
		}
		fail := e.Integration.Classify(resp.Err)
		r.Outcome, r.Reason = gen.FetchOutcomeFailedTemporary, reasonOf(fail, resp.Err)
		if !fail.Temporary {
			r.Outcome = gen.FetchOutcomeFailedPermanent
			blocks = append(blocks, identifierBlock(*r))
		}
	}
	return blocks
}

// requestFailed records a failure of the request itself against every key it
// carried. A temporary failure leaves no block; a permanent one blocks at the
// scope the integration gives it.
func requestFailed[Q, P any](fail Failure, err error, results []Result[Q, P], batch []int, attempts int) []gen.CreateDatasourceBlockParams {
	outcome := gen.FetchOutcomeFailedPermanent
	if fail.Temporary {
		outcome = gen.FetchOutcomeFailedTemporary
	}
	for _, i := range batch {
		results[i].Outcome, results[i].Reason = outcome, reasonOf(fail, err)
		results[i].attempts = attempts
	}
	if fail.Temporary {
		return nil
	}
	var blocks []gen.CreateDatasourceBlockParams
	if fail.Scope == gen.BlockScopeDatasource {
		return append(blocks, gen.CreateDatasourceBlockParams{
			Scope: gen.BlockScopeDatasource, Reason: reasonOf(fail, err), FetchKeyID: results[batch[0]].ID,
		})
	}
	for _, i := range batch {
		blocks = append(blocks, identifierBlock(results[i]))
	}
	return blocks
}

func identifierBlock[Q, P any](r Result[Q, P]) gen.CreateDatasourceBlockParams {
	return gen.CreateDatasourceBlockParams{
		Scope: gen.BlockScopeIdentifier, Reason: r.Reason, FetchKeyID: r.ID,
		SentType: &r.Sent.Type, SentDomain: r.Sent.Domain, SentValue: &r.Sent.Value,
	}
}

func record[Q, P any](ctx context.Context, q Queries, run gen.Run, kind Kind[Q, P], r Result[Q, P]) error {
	arg := gen.CreateFetchKeyParams{
		ID: r.ID, FetchID: run.ID, UserID: run.UserID, StatedKeyID: kind.subject(r.Request),
		Outcome: r.Outcome, Attempts: int16(r.attempts),
	}
	if r.Sent != nil {
		arg.SentType, arg.SentDomain, arg.SentValue = &r.Sent.Type, r.Sent.Domain, &r.Sent.Value
	}
	if r.Outcome == gen.FetchOutcomeServed {
		arg.Candidates = int16(kind.count(r.Response))
	} else {
		reason := r.Reason
		arg.Reason = &reason
	}
	if err := q.CreateFetchKey(ctx, arg); err != nil {
		return fmt.Errorf("create fetch key: %w", err)
	}
	return nil
}

func reasonOf(fail Failure, err error) string {
	if fail.Reason != "" {
		return fail.Reason
	}
	return err.Error()
}

// blockSet is the open blocks of one datasource and kind, read as the fetch
// starts.
type blockSet struct {
	datasource string
	all        bool
	identifier map[types.Identifier]string
}

func newBlockSet(rows []gen.DatasourceBlock) *blockSet {
	b := &blockSet{identifier: map[types.Identifier]string{}}
	for _, row := range rows {
		if row.Scope == gen.BlockScopeDatasource {
			b.all, b.datasource = true, row.Reason
			continue
		}
		if row.SentType != nil && row.SentValue != nil {
			id := types.Identifier{Type: *row.SentType, Domain: row.SentDomain, Value: *row.SentValue}
			b.identifier[id] = row.Reason
		}
	}
	return b
}

// add takes in the blocks a request wrote, so they suppress the requests
// after it.
func (b *blockSet) add(blocks []gen.CreateDatasourceBlockParams) {
	for _, bl := range blocks {
		if bl.Scope == gen.BlockScopeDatasource {
			b.all, b.datasource = true, bl.Reason
			continue
		}
		b.identifier[types.Identifier{Type: *bl.SentType, Domain: bl.SentDomain, Value: *bl.SentValue}] = bl.Reason
	}
}

// covers reports whether a block suppresses a call under id, and why.
func (b *blockSet) covers(id types.Identifier) (string, bool) {
	if b.all {
		return b.datasource, true
	}
	reason, ok := b.identifier[id]
	return reason, ok
}
