package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// FetchKey is one request of a fetch and what it got. It is recorded as a
// fetch_keys row.
type FetchKey[Q, P any] struct {
	Request Q
	// Run is the fetch run that made the request.
	Run gen.Run
	// Source is the datasource that served the request.
	Source string
	// ID is the fetch_keys row, where the consumer attaches its response.
	ID      uuid.UUID
	Outcome gen.FetchOutcome
	// SentID is the identifier the request went out under, nil where the
	// datasource does not serve the request.
	SentID   *types.Identifier
	Reason   string
	Response P
	// AnsweredAt is when the datasource gave Response, zero unless served. An
	// answer from the cache keeps the moment of the call that filled it.
	AnsweredAt time.Time

	// attempts is the number of calls that carried the request.
	attempts int
	// cached reports that the cache answered the request.
	cached bool
}

// Fetcher runs fetches against the datasources of a registry.
type Fetcher struct {
	store Store
	runs  Runner
	cache Cache
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

// WithCache sets the cache a Fetcher reads before it calls. Without one,
// every request misses and nothing is stored.
func WithCache(c Cache) Option {
	return func(f *Fetcher) { f.cache = c }
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

// fetch requests kind about reqs from e, as a child run of parent. It returns
// one fetch key per request, in the order they were given.
func fetch[Q, P any](ctx context.Context, f *Fetcher, parent gen.Run, e *Entry, kind Kind[Q, P], reqs []Q) ([]FetchKey[Q, P], error) {
	var fetchKeys []FetchKey[Q, P]
	_, err := f.runs.Child(ctx, parent, gen.RunKindFetch, func(ctx context.Context, run gen.Run) error {
		var werr error
		fetchKeys, werr = fetchRun(ctx, f, run, e, kind, reqs)
		return werr
	})
	if err != nil {
		return nil, err
	}
	return fetchKeys, nil
}

func fetchRun[Q, P any](ctx context.Context, f *Fetcher, row gen.Run, e *Entry, kind Kind[Q, P], reqs []Q) ([]FetchKey[Q, P], error) {
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
	fetchKeys := make([]FetchKey[Q, P], len(reqs))
	var served []int
	cacheKeys := map[int]string{}
	for i, q := range reqs {
		fetchKeys[i] = FetchKey[Q, P]{Request: q, Run: row, Source: e.Name, ID: db.NewID()}
		if s == nil {
			fetchKeys[i].Outcome, fetchKeys[i].Reason = gen.FetchOutcomeNotServed, fmt.Sprintf("the datasource serves no %s", kind.name)
			continue
		}
		sent, err := s.Serves(q)
		if err != nil {
			fetchKeys[i].Outcome, fetchKeys[i].Reason = gen.FetchOutcomeNotServed, err.Error()
			continue
		}
		fetchKeys[i].SentID = &sent
		p := params(s, q)
		if kind.params != nil {
			p = append(kind.params(q), p...)
		}
		cacheKeys[i] = cacheKey(e.Name, kind.name, sent, p)
		served = append(served, i)
	}
	if err := readCache(ctx, f, fetchKeys, served, cacheKeys); err != nil {
		return nil, err
	}
	var batch []int
	for _, i := range served {
		if fetchKeys[i].cached {
			continue
		}
		if reason, ok := blocked.covers(*fetchKeys[i].SentID); ok {
			fetchKeys[i].Outcome, fetchKeys[i].Reason = gen.FetchOutcomeBlocked, reason
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
			if reason, ok := blocked.covers(*fetchKeys[i].SentID); ok {
				fetchKeys[i].Outcome, fetchKeys[i].Reason = gen.FetchOutcomeBlocked, reason
				continue
			}
			send = append(send, i)
		}
		if len(send) == 0 {
			continue
		}
		blocks := fetchChunk(ctx, f, e, s, fetchKeys, send)
		blocked.add(blocks)
		pending = append(pending, blocks...)
	}
	if err := writeCache(ctx, f, fetchKeys, batch, cacheKeys); err != nil {
		return nil, err
	}
	found := 0
	err = f.store.Tx(ctx, func(q Queries) error {
		for i := range fetchKeys {
			if err := record(ctx, q, row, kind, fetchKeys[i]); err != nil {
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
	for _, r := range fetchKeys {
		instr.key(ctx, e.Name, r.Outcome, r.cached)
	}
	for range found {
		run.Found(ctx, gen.FindingKindBlock)
	}
	return fetchKeys, nil
}

// params returns the parameters s derives from q, nil where s derives none.
func params[Q, P any](s Server[Q, P], q Q) []string {
	if p, ok := s.(Parameterised[Q]); ok {
		return p.Params(q)
	}
	return nil
}

// readCache fills the fetch keys of the requests in served that the cache
// holds. Each is served with zero attempts.
func readCache[Q, P any](ctx context.Context, f *Fetcher, fetchKeys []FetchKey[Q, P], served []int, cacheKeys map[int]string) error {
	if f.cache == nil || len(served) == 0 {
		return nil
	}
	ks := make([]string, len(served))
	for n, i := range served {
		ks[n] = cacheKeys[i]
	}
	entries, err := f.cache.Get(ctx, ks)
	if err != nil {
		return err
	}
	for n, i := range served {
		if entries[n] == nil {
			continue
		}
		r := &fetchKeys[i]
		var entry cacheEntry[P]
		if err := json.Unmarshal(entries[n], &entry); err != nil {
			return fmt.Errorf("decode cached answer: %w", err)
		}
		r.Response, r.AnsweredAt = entry.Answer, entry.At
		r.Outcome, r.cached = gen.FetchOutcomeServed, true
	}
	return nil
}

// writeCache stores the answer of each request in batch that a call served.
func writeCache[Q, P any](ctx context.Context, f *Fetcher, fetchKeys []FetchKey[Q, P], batch []int, cacheKeys map[int]string) error {
	if f.cache == nil {
		return nil
	}
	for _, i := range batch {
		r := &fetchKeys[i]
		if r.Outcome != gen.FetchOutcomeServed {
			continue
		}
		b, err := json.Marshal(cacheEntry[P]{At: r.AnsweredAt, Answer: r.Response})
		if err != nil {
			return fmt.Errorf("encode answer: %w", err)
		}
		if err := f.cache.Set(ctx, cacheKeys[i], b); err != nil {
			return err
		}
	}
	return nil
}

// fetchChunk sends the requests of batch and fills their fetch keys. It
// returns the blocks its failures require, which are written once their fetch
// keys exist.
func fetchChunk[Q, P any](ctx context.Context, f *Fetcher, e *Entry, s Server[Q, P], fetchKeys []FetchKey[Q, P], batch []int) []gen.CreateDatasourceBlockParams {
	reqs := make([]Request[Q], len(batch))
	for n, i := range batch {
		reqs[n] = Request[Q]{Value: fetchKeys[i].Request, Sent: *fetchKeys[i].SentID}
	}
	resps, attempts, err := request(ctx, f, e, s, reqs)
	var paused pausedError
	if errors.As(err, &paused) {
		return requestFailed(Failure{Temporary: true, Scope: ScopeDatasource, Reason: string(paused)}, err, fetchKeys, batch, attempts)
	}
	if err != nil {
		return requestFailed(e.Integration.Classify(err), err, fetchKeys, batch, attempts)
	}

	var blocks []gen.CreateDatasourceBlockParams
	for n, i := range batch {
		r := &fetchKeys[i]
		r.attempts = attempts
		if n >= len(resps) {
			r.Outcome, r.Reason = gen.FetchOutcomeFailedTemporary, "the datasource returned fewer results than it was sent"
			continue
		}
		resp := resps[n]
		if resp.Err == nil {
			r.Outcome, r.Response, r.AnsweredAt = gen.FetchOutcomeServed, resp.Value, f.now()
			continue
		}
		fail := e.Integration.Classify(resp.Err)
		r.Outcome, r.Reason = gen.FetchOutcomeFailedTemporary, reasonOf(fail, resp.Err)
		if fail.Temporary {
			continue
		}
		r.Outcome = gen.FetchOutcomeFailedPermanent
		if fail.Scope != ScopeRequest {
			blocks = append(blocks, identifierBlock(*r))
		}
	}
	return blocks
}

// requestFailed records a failure of the request itself against every key it
// carried. Only a permanent failure outside ScopeRequest blocks, at the scope
// the integration gives it.
func requestFailed[Q, P any](fail Failure, err error, fetchKeys []FetchKey[Q, P], batch []int, attempts int) []gen.CreateDatasourceBlockParams {
	outcome := gen.FetchOutcomeFailedPermanent
	if fail.Temporary {
		outcome = gen.FetchOutcomeFailedTemporary
	}
	for _, i := range batch {
		fetchKeys[i].Outcome, fetchKeys[i].Reason = outcome, reasonOf(fail, err)
		fetchKeys[i].attempts = attempts
	}
	if fail.Temporary || fail.Scope == ScopeRequest {
		return nil
	}
	var blocks []gen.CreateDatasourceBlockParams
	if fail.Scope == ScopeDatasource {
		return append(blocks, gen.CreateDatasourceBlockParams{
			Scope: gen.BlockScopeDatasource, Reason: reasonOf(fail, err), FetchKeyID: fetchKeys[batch[0]].ID,
		})
	}
	for _, i := range batch {
		blocks = append(blocks, identifierBlock(fetchKeys[i]))
	}
	return blocks
}

// identifierBlock blocks what r was sent as: an identifier, or a domain.
func identifierBlock[Q, P any](r FetchKey[Q, P]) gen.CreateDatasourceBlockParams {
	b := gen.CreateDatasourceBlockParams{Scope: gen.BlockScopeIdentifier, Reason: r.Reason, FetchKeyID: r.ID}
	if r.SentID.IsDomain() {
		b.Scope = gen.BlockScopeDomain
	}
	b.SentType, b.SentDomain, b.SentValue = sentColumns(*r.SentID)
	return b
}

// sentColumns writes id as the sent columns of a row. A domain has no value.
func sentColumns(id types.Identifier) (*types.IdentifierType, string, *string) {
	if id.IsDomain() {
		return &id.Type, id.Domain, nil
	}
	return &id.Type, id.Domain, &id.Value
}

func record[Q, P any](ctx context.Context, q Queries, run gen.Run, kind Kind[Q, P], r FetchKey[Q, P]) error {
	arg := gen.CreateFetchKeyParams{
		ID: r.ID, FetchID: run.ID, UserID: run.UserID, StatedKeyID: kind.statedKey(r.Request),
		Outcome: r.Outcome, Attempts: int16(r.attempts),
	}
	if r.SentID != nil {
		arg.SentType, arg.SentDomain, arg.SentValue = sentColumns(*r.SentID)
	}
	if r.Outcome == gen.FetchOutcomeServed {
		arg.Candidates = int16(kind.count(r.Response))
		at := r.AnsweredAt
		arg.AnsweredAt = &at
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
// starts. A domain block is held as the domain's identifier. It suppresses a
// send of the domain and leaves a send of one value alone.
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
		if id, ok := to.BlockSent(row); ok {
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
		id := types.Identifier{Type: *bl.SentType, Domain: bl.SentDomain}
		if bl.SentValue != nil {
			id.Value = *bl.SentValue
		}
		b.identifier[id] = bl.Reason
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
