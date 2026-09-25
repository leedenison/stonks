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

// Result is what one request of a fetch got. ID is the fetch_keys row, which
// the consumer attaches its response to.
type Result[Q, P any] struct {
	Request  Q
	ID       uuid.UUID
	Outcome  gen.FetchOutcome
	Sent     *types.Identifier
	Reason   string
	Response P

	// attempts is the number of calls the request rode in.
	attempts int
}

// Fetcher runs fetches against the datasources of a registry.
type Fetcher struct {
	store Store
	runs  Runner
	log   *slog.Logger

	base  time.Duration
	tries int
}

// Option adjusts a Fetcher.
type Option func(*Fetcher)

// WithRetry sets the first wait after a temporary failure and the number of
// calls one request may cost.
func WithRetry(base time.Duration, tries int) Option {
	return func(f *Fetcher) { f.base, f.tries = base, tries }
}

// NewFetcher returns a Fetcher over store.
func NewFetcher(store Store, runs Runner, log *slog.Logger, opts ...Option) *Fetcher {
	f := &Fetcher{store: store, runs: runs, log: log, base: retryBase, tries: retryTries}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Fetch asks e for kind about reqs, as a child run of parent. It answers one
// result per request, in the order they were given, whether or not the run
// failed.
func Fetch[Q, P any](ctx context.Context, f *Fetcher, parent gen.Run, e *Entry, kind Kind[Q, P], reqs []Q) (gen.Run, []Result[Q, P], error) {
	var results []Result[Q, P]
	row, err := f.runs.Child(ctx, parent, gen.RunKindFetch, func(ctx context.Context, run gen.Run) error {
		var werr error
		results, werr = fetch(ctx, f, run, e, kind, reqs)
		return werr
	})
	return row, results, err
}

func fetch[Q, P any](ctx context.Context, f *Fetcher, row gen.Run, e *Entry, kind Kind[Q, P], reqs []Q) ([]Result[Q, P], error) {
	if _, err := f.store.CreateFetch(ctx, gen.CreateFetchParams{
		ID: row.ID, UserID: row.UserID, Datasource: e.Name, Kind: kind.name,
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
		results[i] = Result[Q, P]{Request: q, ID: db.NewID()}
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

	var pending []gen.CreateDatasourceBlockParams
	for _, chunk := range chunks(batch, e.Integration.Batch()) {
		if blocked.all {
			for _, i := range chunk {
				results[i].Outcome, results[i].Reason = gen.FetchOutcomeBlocked, blocked.datasource
			}
			continue
		}
		blocks := fetchChunk(ctx, f, e, s, results, chunk)
		for _, b := range blocks {
			if b.Scope == gen.BlockScopeDatasource {
				blocked.all, blocked.datasource = true, b.Reason
			}
		}
		pending = append(pending, blocks...)
	}
	for i := range results {
		if err := record(ctx, f, row, kind, results[i]); err != nil {
			return results, err
		}
		instr.key(ctx, e.Name, results[i].Outcome)
	}
	for _, b := range pending {
		b.ID, b.Datasource, b.Kind = db.NewID(), e.Name, kind.name
		b.FindingID, b.RunID = db.NewID(), row.ID
		n, err := f.store.CreateDatasourceBlock(ctx, b)
		if err != nil {
			return results, fmt.Errorf("create block: %w", err)
		}
		if n > 0 {
			run.Found(ctx, gen.FindingKindBlock)
		}
	}
	return results, nil
}

// fetchChunk sends the requests of batch and fills their results. It returns the
// blocks its failures call for, which are written once their fetch keys exist.
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
			r.Outcome, r.Reason = gen.FetchOutcomeFailedTemporary, "the datasource answered fewer results than it was asked for"
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
// carried. A block rests on what the integration says the failure was about:
// the whole datasource, or each identifier the request named.
func requestFailed[Q, P any](fail Failure, err error, results []Result[Q, P], batch []int, attempts int) []gen.CreateDatasourceBlockParams {
	outcome := gen.FetchOutcomeFailedPermanent
	if fail.Temporary {
		outcome = gen.FetchOutcomeFailedTemporary
	}
	for _, i := range batch {
		results[i].Outcome, results[i].Reason = outcome, reasonOf(fail, err)
		results[i].attempts = attempts
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

func record[Q, P any](ctx context.Context, f *Fetcher, run gen.Run, kind Kind[Q, P], r Result[Q, P]) error {
	arg := gen.CreateFetchKeyParams{
		ID: r.ID, FetchID: run.ID, UserID: run.UserID, StatedKeyID: kind.subject(r.Request),
		Outcome: r.Outcome, Attempts: int16(r.attempts),
	}
	if r.Sent != nil {
		arg.SentType, arg.SentDomain, arg.SentValue = &r.Sent.Type, r.Sent.Domain, &r.Sent.Value
	}
	if r.Outcome != gen.FetchOutcomeServed {
		reason := r.Reason
		arg.Reason = &reason
	}
	if err := f.store.CreateFetchKey(ctx, arg); err != nil {
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

// covers reports whether a block suppresses a call under id, and why.
func (b *blockSet) covers(id types.Identifier) (string, bool) {
	if b.all {
		return b.datasource, true
	}
	reason, ok := b.identifier[id]
	return reason, ok
}
