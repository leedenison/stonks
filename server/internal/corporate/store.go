package corporate

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/validity"
	"github.com/leedenison/stonks/server/internal/market"
)

// ReadQueries read the stored answers of tickers.
type ReadQueries interface {
	ListCorporateAnswers(ctx context.Context, arg gen.ListCorporateAnswersParams) ([]gen.ListCorporateAnswersRow, error)
	ListCorporateEvents(ctx context.Context, arg gen.ListCorporateEventsParams) ([]gen.CorporateEvent, error)
}

var _ ReadQueries = (*gen.Queries)(nil)

// Queries is this package's view of the generated queries.
type Queries interface {
	ReadQueries
	LockCorporateAnswer(ctx context.Context, key string) error
	GetCorporateAnswer(ctx context.Context, arg gen.GetCorporateAnswerParams) (gen.GetCorporateAnswerRow, error)
	CreateCorporateAnswer(ctx context.Context, arg gen.CreateCorporateAnswerParams) error
	UpdateCorporateAnswer(ctx context.Context, arg gen.UpdateCorporateAnswerParams) error
	ListAnswerEvents(ctx context.Context, answerID uuid.UUID) ([]gen.CorporateEvent, error)
	CreateCorporateEvent(ctx context.Context, arg gen.CreateCorporateEventParams) error
	DeleteCorporateEvents(ctx context.Context, ids []uuid.UUID) error
	ListDisagreedEvents(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
	CreateFinding(ctx context.Context, arg gen.CreateFindingParams) error
	validity.FactQueries
	ListStatedSplits(ctx context.Context, arg gen.ListStatedSplitsParams) ([]gen.ListStatedSplitsRow, error)
	ListTickersNaming(ctx context.Context, instruments []uuid.UUID) ([]gen.ListTickersNamingRow, error)
	ListUnmatchedSplits(ctx context.Context, keys []uuid.UUID) ([]gen.ListUnmatchedSplitsRow, error)
	ListInstrumentsByID(ctx context.Context, ids []uuid.UUID) ([]gen.Instrument, error)
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
	Enabled() []*market.Entry
}

var _ Sources = (*market.Registry)(nil)

// Fetcher is this package's view of the fetch framework.
type Fetcher interface {
	CorporateEvents(ctx context.Context, parent gen.Run, e *market.Entry, reqs []market.CorporateEventRequest) ([]answerKey, error)
}

var _ Fetcher = (*market.Fetcher)(nil)
