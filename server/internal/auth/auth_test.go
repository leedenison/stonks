package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

func TestGuards(t *testing.T) {
	admin := Principal{User: gen.User{Role: gen.UserRoleAdmin}, SessionID: "a"}
	user := Principal{User: gen.User{Role: gen.UserRoleUser}, SessionID: "u"}
	tests := []struct {
		name      string
		ctx       context.Context
		wantUser  error
		wantAdmin error
	}{
		{name: "anonymous", ctx: context.Background(), wantUser: ErrUnauthenticated, wantAdmin: ErrUnauthenticated},
		{name: "user", ctx: WithPrincipal(context.Background(), user), wantAdmin: ErrPermissionDenied},
		{name: "admin", ctx: WithPrincipal(context.Background(), admin)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := User(tc.ctx)
			if !errors.Is(err, tc.wantUser) {
				t.Errorf("User() error = %v, want %v", err, tc.wantUser)
			}
			if want, _ := PrincipalFrom(tc.ctx); err == nil && !cmp.Equal(want, p) {
				t.Errorf("User() = %+v, want %+v", p, want)
			}
			if _, err := Admin(tc.ctx); !errors.Is(err, tc.wantAdmin) {
				t.Errorf("Admin() error = %v, want %v", err, tc.wantAdmin)
			}
		})
	}
}
