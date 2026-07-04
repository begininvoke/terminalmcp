package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"terminalmcp/internal/model"
)

// ErrNotFound is returned when an engagement does not exist.
var ErrNotFound = errors.New("engagement not found")

// Store is an engagement store with optional JSON-file persistence. Engagements
// are kept in memory and flushed to <dir>/<id>.json by a debounced background
// saver, so history survives a restart.
type Store struct {
	mu          sync.RWMutex
	engagements map[string]*model.Engagement
	dir         string
	dirty       map[string]bool
}

// New creates a store. If dir is non-empty, existing engagements are loaded from
// it and future changes are persisted there.
func New(dir string) *Store {
	s := &Store{
		engagements: make(map[string]*model.Engagement),
		dir:         dir,
		dirty:       make(map[string]bool),
	}
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		s.loadAll()
	}
	return s
}

func (s *Store) loadAll() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var eng model.Engagement
		if err := json.Unmarshal(data, &eng); err != nil {
			continue
		}
		// An engagement that was mid-run when the server stopped is no longer
		// running — mark it interrupted so the UI doesn't show it as live.
		if eng.Status == "running" || eng.Status == "awaiting_input" {
			eng.Status = "interrupted"
		}
		s.engagements[eng.ID] = &eng
	}
}

func (s *Store) Put(e *model.Engagement) {
	s.mu.Lock()
	s.engagements[e.ID] = e
	s.dirty[e.ID] = true
	s.mu.Unlock()
}

func (s *Store) Get(id string) (*model.Engagement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.engagements[id]
	if !ok {
		return nil, ErrNotFound
	}
	return e, nil
}

// Summary is the lightweight record returned by List (no heavy events slice).
type Summary struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Status     string              `json:"status"`
	Phase      string              `json:"phase"`
	Roadmap    []model.RoadmapStep `json:"roadmap"`
	Findings   int                 `json:"findings"`
	CreatedAt  time.Time           `json:"created_at"`
	FinishedAt *time.Time          `json:"finished_at,omitempty"`
}

// List returns engagement summaries, newest first.
func (s *Store) List() []Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Summary, 0, len(s.engagements))
	for _, e := range s.engagements {
		out = append(out, Summary{
			ID: e.ID, Name: e.Name, Status: e.Status, Phase: e.Phase,
			Roadmap: e.Roadmap, Findings: len(e.Findings),
			CreatedAt: e.CreatedAt, FinishedAt: e.FinishedAt,
		})
	}
	// simple insertion sort by CreatedAt desc (engagement counts stay small)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Update runs fn under the store lock for atomic mutation of an engagement.
func (s *Store) Update(id string, fn func(*model.Engagement)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.engagements[id]
	if !ok {
		return ErrNotFound
	}
	fn(e)
	s.dirty[id] = true
	return nil
}

// StartSaver flushes dirty engagements to disk periodically until ctx is done.
func (s *Store) StartSaver(ctx context.Context) {
	if s.dir == "" {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.flush()
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

func (s *Store) flush() {
	s.mu.Lock()
	pending := make(map[string][]byte)
	for id := range s.dirty {
		if e, ok := s.engagements[id]; ok {
			if b, err := json.Marshal(e); err == nil {
				pending[id] = b
			}
		}
		delete(s.dirty, id)
	}
	s.mu.Unlock()

	for id, b := range pending {
		_ = os.WriteFile(filepath.Join(s.dir, id+".json"), b, 0o644)
	}
}
