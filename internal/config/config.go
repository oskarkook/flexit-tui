// Package config persists known units and the last selection between runs.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/oskarkook/flexit-tui/internal/bacnet"
)

type Device struct {
	Key      string    `json:"key"` // serial number, or address when unknown
	Addr     string    `json:"addr"`
	DeviceID uint32    `json:"device_id"`
	Title    string    `json:"title"`
	Serial   string    `json:"serial,omitempty"`
	LastSeen time.Time `json:"last_seen"`

	// Point ids resolved by object name, valid for ResolvedFor firmware.
	Resolved    map[string]bacnet.ObjectID `json:"resolved,omitempty"`
	ResolvedFor string                     `json:"resolved_for,omitempty"`
}

type State struct {
	// Selected is the key of the unit the user last chose explicitly.
	Selected string   `json:"selected,omitempty"`
	Devices  []Device `json:"devices,omitempty"`

	path string
}

// Path returns the state file location ($XDG_STATE_HOME/flexit-tui/state.json,
// defaulting to ~/.local/state/flexit-tui/state.json).
func Path() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "flexit-tui", "state.json"), nil
}

// Load reads the state file; a missing file yields an empty state.
func Load(path string) (*State, error) {
	s := &State{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return &State{path: path}, err
	}
	return s, nil
}

// Save writes the state atomically.
func (s *State) Save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *State) Find(key string) *Device {
	for i := range s.Devices {
		if s.Devices[i].Key == key {
			return &s.Devices[i]
		}
	}
	return nil
}

// Upsert records a unit, keeping its cached point resolution.
func (s *State) Upsert(d Device) *Device {
	if cur := s.Find(d.Key); cur != nil {
		if d.Resolved == nil {
			d.Resolved, d.ResolvedFor = cur.Resolved, cur.ResolvedFor
		}
		*cur = d
		return cur
	}
	s.Devices = append(s.Devices, d)
	return &s.Devices[len(s.Devices)-1]
}

// Remove forgets a unit.
func (s *State) Remove(key string) {
	for i := range s.Devices {
		if s.Devices[i].Key == key {
			s.Devices = append(s.Devices[:i], s.Devices[i+1:]...)
			break
		}
	}
	if s.Selected == key {
		s.Selected = ""
	}
}
