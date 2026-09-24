package statement

import (
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// parseDate reads an ISO 8601 date as midnight UTC.
func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation(time.DateOnly, s, time.UTC)
}

// validate reads r, and either rejects it with the first failing check as
// its reason or interns its key.
func (g *ingestion) validate(ordinal int32, r *statementv1.Row, today time.Time, currencies map[string]bool) row {
	out := row{ordinal: ordinal, stated: r}
	reject := func(format string, args ...any) row {
		out.reason = fmt.Sprintf(format, args...)
		return out
	}
	if r.GetKey() == nil {
		return reject("no key")
	}
	k, err := keyOf(r.GetKey())
	if err != nil {
		return reject("%v", err)
	}
	if !k.statable() {
		return reject("no identifier or description")
	}
	dates := []struct {
		name string
		s    string
		dst  *time.Time
	}{{"order date", r.GetOrderDate(), &out.order}, {"settlement date", r.GetSettlementDate(), &out.settlement}, {"as at", r.GetAsAt(), &out.asAt}}
	for _, d := range dates {
		if *d.dst, err = parseDate(d.s); err != nil {
			return reject("malformed %s %q", d.name, d.s)
		}
	}
	if out.quantity, err = decimal.NewFromString(r.GetQuantity()); err != nil {
		return reject("malformed quantity %q", r.GetQuantity())
	}
	if out.order.Before(g.from) || !out.order.Before(g.before) {
		return reject("order date outside the claimed period")
	}
	if out.order.After(today) {
		return reject("order date after today")
	}
	if k.currency != nil && !currencies[*k.currency] {
		return reject("unknown currency %q", *k.currency)
	}
	out.key = g.intern(k)
	return out
}

// split reads sp, whose key must pass the checks a row's does.
func (g *ingestion) split(ordinal int32, sp *statementv1.StatedSplit, currencies map[string]bool) (split, error) {
	out := split{ordinal: ordinal}
	if sp.GetKey() == nil {
		return out, fmt.Errorf("no key")
	}
	k, err := keyOf(sp.GetKey())
	if err != nil {
		return out, err
	}
	if !k.statable() {
		return out, fmt.Errorf("no identifier or description")
	}
	if k.currency != nil && !currencies[*k.currency] {
		return out, fmt.Errorf("unknown currency %q", *k.currency)
	}
	if out.effective, err = parseDate(sp.GetEffectiveDate()); err != nil {
		return out, fmt.Errorf("malformed effective date %q", sp.GetEffectiveDate())
	}
	if out.quantity, err = decimal.NewFromString(sp.GetQuantity()); err != nil {
		return out, fmt.Errorf("malformed quantity %q", sp.GetQuantity())
	}
	if r := sp.GetRatio(); r != nil {
		from, err := decimal.NewFromString(r.GetFrom())
		if err != nil {
			return out, fmt.Errorf("malformed ratio from %q", r.GetFrom())
		}
		to, err := decimal.NewFromString(r.GetTo())
		if err != nil {
			return out, fmt.Errorf("malformed ratio to %q", r.GetTo())
		}
		out.from, out.to = &from, &to
	}
	out.key = g.intern(k)
	return out, nil
}

// keyOf reads a stated key into its canonical form: the class and
// identifier types in the database vocabulary, an empty description as
// none, and the identifiers sorted by type, domain and value with an
// absent domain first. It fails a class or type outside the vocabulary and
// two identifiers of one type and domain.
func keyOf(sk *typev1.StatedKey) (*key, error) {
	class, ok := db.FromProto[gen.AssetClass](sk.GetAssetClass())
	if !ok {
		return nil, fmt.Errorf("asset class %d outside the vocabulary", sk.GetAssetClass())
	}
	k := &key{currency: sk.Currency, description: sk.Description, identifiers: []types.StatedIdentifier{}}
	if sk.GetDescription() == "" {
		k.description = nil
	}
	if class != "" {
		k.class = &class
	}
	for _, id := range sk.GetIdentifiers() {
		t, ok := db.FromProto[gen.IdentifierType](id.GetType())
		if !ok || t == "" {
			return nil, fmt.Errorf("identifier type %d outside the vocabulary", id.GetType())
		}
		for _, held := range k.identifiers {
			if held.Type == string(t) && held.Domain == id.GetDomain() {
				return nil, fmt.Errorf("two %s identifiers, %s and %s", t, held.Value, id.GetValue())
			}
		}
		k.identifiers = append(k.identifiers, types.StatedIdentifier{Type: string(t), Domain: id.GetDomain(), Value: id.GetValue()})
	}
	sort.Slice(k.identifiers, func(i, j int) bool {
		a, b := k.identifiers[i], k.identifiers[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		return a.Value < b.Value
	})
	return k, nil
}
