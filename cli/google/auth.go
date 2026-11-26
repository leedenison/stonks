package google

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
)

// scopes are what the tool asks of Google: the identity of the administrator,
// and the files the tool itself created.
var scopes = []string{"openid", "email", "https://www.googleapis.com/auth/drive.file"}

// ErrSignedOut reports that the state holds no refresh token, so only a
// sign-in in the browser can produce a token.
var ErrSignedOut = errors.New("signed out of Google")

// Client is the OAuth desktop client that Google issued for the tool.
type Client struct {
	ID       string `json:"client_id"`
	Secret   string `json:"client_secret"`
	AuthURI  string `json:"auth_uri"`
	TokenURI string `json:"token_uri"`
}

// LoadClient reads the client file that the Google Cloud console offers for
// download.
func LoadClient(path string) (Client, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Client{}, fmt.Errorf("read the OAuth client: %w", err)
	}
	var file struct {
		Installed *Client `json:"installed"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return Client{}, fmt.Errorf("read the OAuth client %s: %w", path, err)
	}
	c := file.Installed
	if c == nil || c.ID == "" || c.AuthURI == "" || c.TokenURI == "" {
		return Client{}, fmt.Errorf("read the OAuth client %s: not the file of a desktop client", path)
	}
	return *c, nil
}

func (c Client) config(redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: c.ID, ClientSecret: c.Secret, Scopes: scopes, RedirectURL: redirect,
		Endpoint: oauth2.Endpoint{AuthURL: c.AuthURI, TokenURL: c.TokenURI, AuthStyle: oauth2.AuthStyleInParams},
	}
}

// Source returns the tokens of the Sheets calls, refreshed from the state in
// dir. It never opens a sign-in: without a refresh token it fails with
// ErrSignedOut.
func Source(ctx context.Context, c Client, dir string) (oauth2.TokenSource, error) {
	s, err := load(dir)
	if err != nil {
		return nil, err
	}
	if s.RefreshToken == "" {
		return nil, ErrSignedOut
	}
	return c.config("").TokenSource(ctx, &oauth2.Token{RefreshToken: s.RefreshToken}), nil
}

// Identity returns a Google ID token of the administrator. It refreshes the
// token from the state in dir. It runs the sign-in in the browser when the
// state holds no refresh token, when Google has withdrawn the token, and when
// the refresh answers without an ID token.
//
// The sign-in waits for the redirect on the listen address and writes the
// address of the sign-in to out. It stores the new refresh token in the state.
func Identity(ctx context.Context, c Client, dir, listen string, out io.Writer) (string, error) {
	s, err := load(dir)
	if err != nil {
		return "", err
	}
	if s.RefreshToken != "" {
		tok, err := c.config("").TokenSource(ctx, &oauth2.Token{RefreshToken: s.RefreshToken}).Token()
		var refused *oauth2.RetrieveError
		switch {
		case err == nil:
			if id := idToken(tok); id != "" {
				return id, nil
			}
		case errors.As(err, &refused) && refused.ErrorCode == "invalid_grant":
		default:
			return "", fmt.Errorf("refresh the Google token: %w", err)
		}
	}
	tok, err := c.signIn(ctx, listen, out)
	if err != nil {
		return "", err
	}
	id := idToken(tok)
	if id == "" {
		return "", errors.New("sign in to Google: the answer lacks an ID token")
	}
	if tok.RefreshToken != "" {
		s.RefreshToken = tok.RefreshToken
		if err := s.save(dir); err != nil {
			return "", err
		}
	}
	return id, nil
}

func idToken(tok *oauth2.Token) string {
	id, _ := tok.Extra("id_token").(string)
	return id
}

// linger is how long the sign-in listener stays up after the redirect, so
// that its answer reaches the browser.
const linger = 2 * time.Second

// redirected is what the browser brought back from Google.
type redirected struct {
	code string
	err  error
}

// signIn runs the authorisation code flow of a desktop app, with PKCE and a
// loopback redirect.
func (c Client) signIn(ctx context.Context, listen string, out io.Writer) (tok *oauth2.Token, err error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("sign in to Google: %w", err)
	}
	cfg := c.config("http://" + ln.Addr().String())
	verifier, state := oauth2.GenerateVerifier(), rand.Text()
	got := make(chan redirected, 1)
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var res redirected
		switch {
		case q.Get("state") != state:
			res.err = errors.New("the redirect carries another state")
		case q.Get("error") != "":
			res.err = fmt.Errorf("the answer of Google is %s", q.Get("error"))
		case q.Get("code") == "":
			res.err = errors.New("the redirect lacks a code")
		default:
			res.code = q.Get("code")
		}
		if res.err != nil {
			http.Error(w, "Sign-in failed: "+res.err.Error(), http.StatusBadRequest)
		} else if _, err := io.WriteString(w, "Signed in. You can close this tab."); err != nil {
			res.err = err
		}
		select {
		case got <- res:
		default:
		}
	})}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	defer func() {
		// The shutdown outlives a cancelled ctx so the listener always closes.
		// It gives the answer to the browser time to go out. A browser also
		// opens spare connections that never send a request, and a shutdown
		// waits for those, so they are closed once the time is up.
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), linger)
		defer cancel()
		serr := srv.Shutdown(stop)
		if errors.Is(serr, context.DeadlineExceeded) {
			serr = srv.Close()
		}
		err = errors.Join(err, serr)
		<-served
	}()

	// Google returns a refresh token only for offline access, and only when
	// the user has just consented.
	url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "consent"))
	if _, err := fmt.Fprintf(out, "Open this address in a browser to sign in to Google:\n\n%s\n\n", url); err != nil {
		return nil, err
	}
	select {
	case res := <-got:
		if res.err != nil {
			return nil, fmt.Errorf("sign in to Google: %w", res.err)
		}
		tok, err = cfg.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, fmt.Errorf("sign in to Google: %w", err)
		}
		return tok, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("sign in to Google: %w", ctx.Err())
	}
}
