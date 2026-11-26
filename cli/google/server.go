package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
)

// SignIn signs in to the Stonks service at base with a Google ID token, and
// returns a price client that carries the session. It refuses an account
// that is not an administrator, since every procedure the tool calls would.
func SignIn(ctx context.Context, h *http.Client, base, idToken string) (adminpricev1connect.PriceServiceClient, error) {
	res, err := authv1connect.NewAuthServiceClient(h, base).SignIn(ctx, connect.NewRequest(&authv1.SignInRequest{GoogleIdToken: idToken}))
	if err != nil {
		return nil, fmt.Errorf("sign in to %s: %w", base, err)
	}
	if res.Msg.GetUser().GetRole() != authv1.Role_ROLE_ADMIN {
		return nil, fmt.Errorf("sign in to %s: %s is not an administrator", base, res.Msg.GetUser().GetEmail())
	}
	cookie, err := http.ParseSetCookie(res.Header().Get("Set-Cookie"))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("sign in to %s: the answer lacks a session", base), err)
	}
	session := cookie.Name + "=" + cookie.Value
	carry := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Cookie", session)
			return next(ctx, req)
		}
	})
	return adminpricev1connect.NewPriceServiceClient(h, base, connect.WithInterceptors(carry)), nil
}
