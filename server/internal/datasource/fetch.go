package datasource

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/run"
)

const (
	retryBase  = 250 * time.Millisecond
	retryMax   = 2 * time.Second
	retryTries = 3
)

// KeyResult is what one key of a fetch got. ID is the fetch_keys row, which
// the consumer attaches its answer to.
type KeyResult struct {
	Key        StatedKey
	ID         uuid.UUID
	Outcome    gen.FetchOutcome
	Sent       *types.Identifier
	Filtered   []types.Identifier
	Candidates []Candidate
	Reason     string

	// attempts is the number of requests the key rode in.
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

// Fetch asks e for kind about keys, as a child run of parent. It answers one
// result per key, in the order they were given, whether or not the run
// failed.
func (f *Fetcher) Fetch(ctx context.Context, parent gen.Run, e *Entry, kind gen.FetchKind, keys []StatedKey) (gen.Run, []KeyResult, error) {
	var results []KeyResult
	row, err := f.runs.Child(ctx, parent, gen.RunKindFetch, func(ctx context.Context, run gen.Run) error {
		var werr error
		results, werr = f.fetch(ctx, run, e, kind, keys)
		return werr
	})
	return row, results, err
}

func (f *Fetcher) fetch(ctx context.Context, row gen.Run, e *Entry, kind gen.FetchKind, keys []StatedKey) ([]KeyResult, error) {
	if _, err := f.store.CreateFetch(ctx, gen.CreateFetchParams{
		ID: row.ID, UserID: row.UserID, Datasource: e.Name, Kind: kind,
	}); err != nil {
		return nil, fmt.Errorf("create fetch: %w", err)
	}
	open, err := f.store.ListOpenBlocks(ctx, gen.ListOpenBlocksParams{Datasource: e.Name, Kind: kind})
	if err != nil {
		return nil, fmt.Errorf("list open blocks: %w", err)
	}
	blocked := newBlockSet(open)

	results := make([]KeyResult, len(keys))
	var batch []int
	for i, k := range keys {
		results[i] = KeyResult{Key: k, ID: db.NewID()}
		sent, err := e.Identity.Serves(k)
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

	var blocks []gen.CreateDatasourceBlockParams
	for _, chunk := range chunks(batch, e.Integration.Batch()) {
		if blocked.all {
			for _, i := range chunk {
				results[i].Outcome, results[i].Reason = gen.FetchOutcomeBlocked, blocked.datasource
			}
			continue
		}
		earned := f.call(ctx, e, results, chunk)
		for _, b := range earned {
			if b.Scope == gen.BlockScopeDatasource {
				blocked.all, blocked.datasource = true, b.Reason
			}
		}
		blocks = append(blocks, earned...)
	}
	for i := range results {
		if err := f.record(ctx, row, results[i]); err != nil {
			return results, err
		}
		instr.key(ctx, e.Name, results[i].Outcome)
	}
	for _, b := range blocks {
		b.ID, b.Datasource, b.Kind = db.NewID(), e.Name, kind
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

// call sends the keys of batch and fills their results. It answers the blocks
// the failures earned, which are written once their fetch keys exist.
func (f *Fetcher) call(ctx context.Context, e *Entry, results []KeyResult, batch []int) []gen.CreateDatasourceBlockParams {
	sent := make([]types.Identifier, len(batch))
	for n, i := range batch {
		sent[n] = *results[i].Sent
	}
	answers, attempts, err := f.request(ctx, e, sent)
	if err != nil {
		return requestFailed(e.Integration.Classify(err), err, results, batch, attempts)
	}

	var blocks []gen.CreateDatasourceBlockParams
	for n, i := range batch {
		r := &results[i]
		r.attempts = attempts
		if n >= len(answers) {
			r.Outcome, r.Reason = gen.FetchOutcomeFailedTemporary, "the datasource answered fewer results than it was asked for"
			continue
		}
		a := answers[n]
		if a.Err == nil {
			r.Outcome, r.Filtered, r.Candidates = gen.FetchOutcomeServed, a.Filtered, a.Candidates
			continue
		}
		fail := e.Integration.Classify(a.Err)
		r.Outcome, r.Reason = gen.FetchOutcomeFailedTemporary, reasonOf(fail, a.Err)
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
func requestFailed(fail Failure, err error, results []KeyResult, batch []int, attempts int) []gen.CreateDatasourceBlockParams {
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

// chunks splits batch into runs of at most n, or one run where n is zero.
func chunks(batch []int, n int) [][]int {
	if len(batch) == 0 {
		return nil
	}
	if n <= 0 {
		return [][]int{batch}
	}
	return slices.Collect(slices.Chunk(batch, n))
}

func identifierBlock(r KeyResult) gen.CreateDatasourceBlockParams {
	return gen.CreateDatasourceBlockParams{
		Scope: gen.BlockScopeIdentifier, Reason: r.Reason, FetchKeyID: r.ID,
		SentType: &r.Sent.Type, SentDomain: r.Sent.Domain, SentValue: &r.Sent.Value,
	}
}

// request calls the provider, waiting on the datasource's limiter before each
// attempt. A temporary failure is retried under an exponential backoff unless
// it is about the datasource, which repeating cannot help.
func (f *Fetcher) request(ctx context.Context, e *Entry, sent []types.Identifier) ([]IdentityResult, int, error) {
	wait := f.base
	for attempt := 1; ; attempt++ {
		if err := e.limiter.Wait(ctx); err != nil {
			return nil, attempt - 1, fmt.Errorf("rate limit: %w", err)
		}
		answers, err := e.Identity.Fetch(ctx, sent)
		if err == nil {
			return answers, attempt, nil
		}
		fail := e.Integration.Classify(err)
		if !fail.Temporary || fail.Scope == gen.BlockScopeDatasource || attempt >= f.tries {
			return nil, attempt, err
		}
		pause := wait + rand.N(wait/2+1)
		if fail.RetryAfter > 0 {
			pause = fail.RetryAfter
		}
		f.log.Debug("retrying a fetch", "datasource", e.Name, "attempt", attempt, "wait", pause)
		select {
		case <-time.After(pause):
		case <-ctx.Done():
			return nil, attempt, ctx.Err()
		}
		if wait *= 2; wait > retryMax {
			wait = retryMax
		}
	}
}

func (f *Fetcher) record(ctx context.Context, run gen.Run, r KeyResult) error {
	arg := gen.CreateFetchKeyParams{
		ID: r.ID, FetchID: run.ID, UserID: run.UserID, StatedKeyID: r.Key.ID,
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
