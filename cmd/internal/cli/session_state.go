package cli

import (
	"encoding/json"
	"os"
)

const sessionStateFile = ".pos-session.json"

type SessionState struct {
	ActiveWorker string `json:"active_worker"`
}

func loadSessionState() (*SessionState, error) {
	data, err := os.ReadFile(sessionStateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionState{}, nil
		}
		return nil, err
	}

	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return &SessionState{}, nil
	}

	return &state, nil
}

func saveSessionState(state *SessionState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(sessionStateFile, data, 0644)
}

func getActiveWorker() (string, error) {
	state, err := loadSessionState()
	if err != nil {
		return "", err
	}
	return state.ActiveWorker, nil
}

func setActiveWorker(workerID string) error {
	state := &SessionState{ActiveWorker: workerID}
	return saveSessionState(state)
}

func clearActiveWorker() error {
	state := &SessionState{}
	return saveSessionState(state)
}
