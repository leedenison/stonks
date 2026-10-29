package corpact

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/market"
)

// ReadQueries read the stored answers of tickers.
type ReadQueries interface {
	ListCorpActAnswers(ctx context.Context, arg gen.ListCorpActAnswersParams) ([]gen.ListCorpActAnswersRow, error)
	ListCorpActs(ctx context.Context, arg gen.ListCorpActsParams) ([]gen.CorpAct, error)
}

var _ ReadQueries = (*gen.Queries)(nil)

// Queries is this package's view of the generated queries.
type Queries interface {
	CalendarQueries
	ListCorpActSources(ctx context.Context) ([]gen.ListCorpActSourcesRow, error)
	LockCorpActAnswer(ctx context.Context, key string) error
	GetCorpActAnswer(ctx context.Context, arg gen.GetCorpActAnswerParams) (gen.GetCorpActAnswerRow, error)
	CreateCorpActAnswer(ctx context.Context, arg gen.CreateCorpActAnswerParams) error
	UpdateCorpActAnswer(ctx context.Context, arg gen.UpdateCorpActAnswerParams) error
	ListAnswerCorpActs(ctx context.Context, answerID uuid.UUID) ([]gen.CorpAct, error)
	CreateCorpAct(ctx context.Context, arg gen.CreateCorpActParams) error
	DeleteCorpActs(ctx context.Context, ids []uuid.UUID) error
	CreateFinding(ctx context.Context, arg gen.CreateFindingParams) error
	tickers.Queries
	ListStatedSplits(ctx context.Context, arg gen.ListStatedSplitsParams) ([]gen.ListStatedSplitsRow, error)
	ListUnmatchedSplits(ctx context.Context, keys []uuid.UUID) ([]gen.ListUnmatchedSplitsRow, error)
	ListInstrsByID(ctx context.Context, ids []uuid.UUID) ([]gen.Instr, error)
}

var _ Queries = (*gen.Queries)(nil)

// Store is the database as this package sees it: the queries, and Tx, which
// runs a set of them in one transaction.
type Store interface {
	Queries
	Tx(ctx context.Context, fn func(Queries) error) error
}

var _ Store = (*db.DB[Queries])(nil)

// Sources is this package's view of the datasource registry.
type Sources interface {
	Entry(name string) *market.Entry
}

var _ Sources = (*market.Registry)(nil)

// Fetcher is this package's view of the fetch framework.
type Fetcher interface {
	CorpActs(ctx context.Context, parent gen.Run, e *market.Entry, reqs []market.CorpActRequest) ([]answerKey, error)
}

var _ Fetcher = (*market.Fetcher)(nil)
