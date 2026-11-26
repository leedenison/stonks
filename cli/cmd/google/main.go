// Command google is the Google Sheets tool. An administrator runs its write
// step, pastes the values of the spreadsheet by hand, and runs its read step;
// see package google.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"

	"github.com/leedenison/stonks/cli/google"
)

// timeout bounds each call to the service and to Google.
const timeout = time.Minute

// listen is where the sign-in waits for the browser: a free port on the
// loopback interface.
const listen = "127.0.0.1:0"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "google:", err)
		os.Exit(1)
	}
}

const usage = `usage: google [-server URL] [-client FILE] write|read

  write  put a formula for each price gap into the spreadsheet
  read   upload the values pasted into the values tab

  -server URL   address of the Stonks service
                (default $STONKS_SERVER, or http://localhost:8080)
  -client FILE  file of the OAuth desktop client that Google issued
                (default $STONKS_GOOGLE_CLIENT_FILE, or local/oauth/stonks-cli-client.json)`

// env returns the environment variable key, or def where it is unset.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("google", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	server := flags.String("server", env("STONKS_SERVER", "http://localhost:8080"), "")
	clientFile := flags.String("client", env("STONKS_GOOGLE_CLIENT_FILE", "local/oauth/stonks-cli-client.json"), "")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	if flags.NArg() != 1 || (flags.Arg(0) != "write" && flags.Arg(0) != "read") {
		return errors.New(usage)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(config, "stonks")
	client, err := google.LoadClient(*clientFile)
	if err != nil {
		return err
	}
	h := &http.Client{Timeout: timeout}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, h)
	id, err := google.Identity(ctx, client, dir, listen, stderr)
	if err != nil {
		return err
	}
	prices, err := google.SignIn(ctx, h, *server, id)
	if err != nil {
		return err
	}
	source, err := google.Source(ctx, client, dir)
	if err != nil {
		return err
	}
	tool := &google.Tool{Sheets: google.NewSheets(oauth2.NewClient(ctx, source), ""), Prices: prices, Dir: dir, Clock: time.Now, Out: stdout}
	if flags.Arg(0) == "write" {
		return tool.Write(ctx)
	}
	return tool.Read(ctx)
}
