// Package to converts generated rows to the types of [types] and to proto
// messages. A conversion is a method on its source type, defined beside it;
// a generated type cannot carry one, so its conversions live here. Each is
// a field copy: a conversion that reads anything else stays with its caller.
package to

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Identifier returns the triple an identifier row names.
func Identifier(row gen.Identifier) types.Identifier {
	return types.Identifier{Type: row.Type, Domain: row.Domain, Value: row.Value}
}

// Sent returns the identifier under which a fetch key was sent, false where
// the key was not sent.
func Sent(k gen.FetchKey) (types.Identifier, bool) {
	return sent(k.SentType, k.SentDomain, k.SentValue)
}

// BlockSent returns the identifier under which a block was raised, false
// where the block is of the whole datasource.
func BlockSent(b gen.DatasourceBlock) (types.Identifier, bool) {
	return sent(b.SentType, b.SentDomain, b.SentValue)
}

func sent(typ *types.IdentifierType, domain string, value *string) (types.Identifier, bool) {
	if typ == nil || value == nil {
		return types.Identifier{}, false
	}
	return types.Identifier{Type: *typ, Domain: domain, Value: *value}, true
}

// ProtoRun writes a run row as its proto message.
func ProtoRun(r gen.Run) *runv1.Run {
	out := &runv1.Run{
		Id:        r.ID.String(),
		Kind:      types.ToProto[runv1.RunKind](r.Kind),
		Trigger:   types.ToProto[runv1.RunTrigger](r.Trigger),
		State:     types.ToProto[runv1.RunState](r.State),
		Error:     r.Error,
		CreatedAt: timestamppb.New(r.CreatedAt),
	}
	if r.ParentID != nil {
		parent := r.ParentID.String()
		out.ParentId = &parent
	}
	if r.StartedAt != nil {
		out.StartedAt = timestamppb.New(*r.StartedAt)
	}
	if r.FinishedAt != nil {
		out.FinishedAt = timestamppb.New(*r.FinishedAt)
	}
	return out
}

// ProtoUser writes a user row as its proto message.
func ProtoUser(u gen.User) *authv1.User {
	return &authv1.User{Id: u.ID.String(), Email: u.Email, Name: u.Name, Role: types.ToProto[authv1.Role](u.Role)}
}

// ProtoStatedKey writes a stated key row as the key its source stated.
func ProtoStatedKey(k gen.StatedKey) *typev1.StatedKey {
	out := &typev1.StatedKey{Currency: k.Currency}
	if k.AssetClass != nil {
		out.AssetClass = types.ToProto[typev1.AssetClass](*k.AssetClass)
	}
	for _, i := range k.Identifiers {
		out.Identifiers = append(out.Identifiers, i.ToProto())
	}
	return out
}

// ProtoResolutionItem writes k with what a resolution made of it. A nil r,
// for a key no resolution has reached, leaves the outcome unset.
func ProtoResolutionItem(k gen.StatedKey, r *gen.ResolutionKey) *typev1.ResolutionItem {
	out := &typev1.ResolutionItem{StatedKey: ProtoStatedKey(k), StatedKeyId: k.ID.String()}
	if r != nil {
		out.Outcome = types.ToProto[typev1.ResolutionOutcome](r.Outcome)
		out.Reasons = r.Reasons
	}
	if k.Arbiter != nil {
		out.Arbiter = types.ToProto[typev1.Arbiter](*k.Arbiter)
	}
	return out
}
