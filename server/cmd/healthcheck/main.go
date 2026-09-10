// Reports whether the service is healthy, for a container healthcheck: exits
// 0 when a GET of the URL named by the one argument answers 200, and 1
// otherwise. Only the status code is asserted.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

const timeout = 2 * time.Second

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := check(ctx, os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
}

func check(ctx context.Context, url string) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, res.Body.Close()) }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", res.Status)
	}
	return nil
}
