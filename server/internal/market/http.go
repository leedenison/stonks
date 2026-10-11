package market

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// StatusError is a provider's response with a status other than 200.
type StatusError struct {
	Provider string
	Code     int
	Header   http.Header
	// Body is the start of the response body, trimmed.
	Body string
}

// Error names the status in words, with the body where there is one:
// "massive returned too many requests".
func (e StatusError) Error() string {
	text := strings.ToLower(http.StatusText(e.Code))
	if text == "" {
		text = strconv.Itoa(e.Code)
	}
	if e.Body == "" {
		return e.Provider + " returned " + text
	}
	return fmt.Sprintf("%s returned %s: %s", e.Provider, text, e.Body)
}

// bodyLimit is how much of a refused response's body a StatusError keeps.
const bodyLimit = 512

// Do sends req and decodes a 200's body into v. Any other status is a
// StatusError. Do strips the URL from a transport error, since the URL may
// carry the credential and the error reaches the fetch records.
func Do(c *http.Client, req *http.Request, provider string, v any) (err error) {
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", provider, WithoutURL(err))
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		text, rerr := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
		status := StatusError{Provider: provider, Code: resp.StatusCode, Header: resp.Header, Body: string(bytes.TrimSpace(text))}
		return errors.Join(status, rerr)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode %s response: %w", provider, err)
	}
	return nil
}

// WithoutURL drops the URL from an error so that API keys are redacted.
func WithoutURL(err error) error {
	var u *url.Error
	if errors.As(err, &u) {
		return u.Err
	}
	return err
}

// Endpoint returns cfg's endpoint or def. It rejects an endpoint that is not
// an http or https URL with a host. Every integration calls it, because the
// registry validates a datasource row only through the integration's
// factory. The check also keeps a credential out of errors: when a request
// URL fails to parse, net/http quotes the URL, credential included.
func Endpoint(cfg Config, def string) (string, error) {
	if cfg.Endpoint == "" {
		return def, nil
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%s endpoint %q is not an http or https URL with a host", cfg.Name, cfg.Endpoint)
	}
	return cfg.Endpoint, nil
}
