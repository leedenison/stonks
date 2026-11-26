package google

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// stateFile is the name of the state file inside the state directory.
const stateFile = "google.json"

// state is what the tool keeps between invocations.
type state struct {
	// RefreshToken is the OAuth refresh token of the administrator. Empty
	// until the first sign-in.
	RefreshToken string `json:"refresh_token,omitempty"`
	// SpreadsheetID names the spreadsheet the tool created. Empty until the
	// first write step.
	SpreadsheetID string `json:"spreadsheet_id,omitempty"`
}

// load reads the state in dir. A missing file is the empty state.
func load(dir string) (state, error) {
	var s state
	b, err := os.ReadFile(filepath.Join(dir, stateFile)) //nolint:gosec // the state directory is the administrator's own
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("read state: %w", err)
	}
	return s, nil
}

// save writes the state to dir, readable by its owner alone because it holds
// a credential.
func (s state) save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	b, err := json.Marshal(s) //nolint:gosec // the state file is where the refresh token is kept
	if err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	path := filepath.Join(dir, stateFile)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	// WriteFile keeps the mode of a file that already exists.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}
