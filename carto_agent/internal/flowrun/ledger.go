package flowrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// PreparationAttempt is an observed acquisition action, not an inferred promise.
type PreparationAttempt struct {
	Params     map[string]any `json:"params,omitempty"`
	Method     string         `json:"method"`
	Action     string         `json:"action"`
	Result     string         `json:"result"`
	Reason     string         `json:"reason,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at,omitempty"`
	Paths      []string       `json:"paths,omitempty"`
}
type DataPreparationState struct {
	StartedAt *time.Time           `json:"started_at,omitempty"`
	DataID    string               `json:"data_id"`
	Name      string               `json:"name"`
	Status    string               `json:"status"`
	Reason    string               `json:"reason,omitempty"`
	Ledger    []PreparationAttempt `json:"ledger"`
}

func SavePreparationLedger(path string, state DataPreparationState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".ledger-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	_, err = file.Write(raw)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
