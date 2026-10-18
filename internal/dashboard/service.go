package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Store persists dashboards. Get and Delete return ErrNotFound for a missing
// dashboard; Insert returns ErrConflict when (project, uid) exists, and
// Update when no row is at prevRevision.
type Store interface {
	List(ctx context.Context, projectID string) ([]Dashboard, error)
	Get(ctx context.Context, projectID, uid string) (Dashboard, error)
	Insert(ctx context.Context, d Dashboard) error
	Update(ctx context.Context, d Dashboard, prevRevision int) error
	Delete(ctx context.Context, projectID, uid string) error
}

// Service manages a project's dashboards.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService returns a service over store, with now as the clock.
func NewService(store Store, now func() time.Time) *Service {
	return &Service{store: store, now: now}
}

// List returns a project's dashboards by title.
func (s *Service) List(ctx context.Context, projectID string) ([]Summary, error) {
	all, err := s.store.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(all))
	for _, d := range all {
		out = append(out, Summary{UID: d.UID, Title: d.Title, Panels: len(d.Panels), Revision: d.Revision, UpdatedAt: d.UpdatedAt})
	}
	return out, nil
}

// Get returns one dashboard, or ErrNotFound.
func (s *Service) Get(ctx context.Context, projectID, uid string) (Dashboard, error) {
	return s.store.Get(ctx, projectID, uid)
}

// Put validates and normalizes def, then creates the dashboard or replaces
// it. expect is nil for an unconditional write, 0 to create only, or the
// revision the writer last saw, which must still be the stored one. A write
// that changes nothing keeps the stored dashboard and its revision, so
// applying the same file twice is a no-op. created reports a new dashboard.
func (s *Service) Put(ctx context.Context, projectID, uid string, def Definition, expect *int) (d Dashboard, created bool, err error) {
	if err := ValidateUID(uid); err != nil {
		return Dashboard{}, false, err
	}
	norm, err := Normalize(def)
	if err != nil {
		return Dashboard{}, false, err
	}
	cur, err := s.store.Get(ctx, projectID, uid)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Dashboard{}, false, err
	}
	if expect != nil {
		switch {
		case *expect < 0:
			return Dashboard{}, false, invalid("revision must be 0 to create, or the revision last read")
		case *expect == 0 && exists:
			return Dashboard{}, false, conflict("dashboard %s already exists", uid)
		case *expect > 0 && !exists:
			return Dashboard{}, false, conflict("dashboard %s no longer exists", uid)
		case *expect > 0 && cur.Revision != *expect:
			return Dashboard{}, false, conflict("dashboard %s is at revision %d, not %d; reload it and try again", uid, cur.Revision, *expect)
		}
	}

	// The store keeps microseconds; returning the same keeps Put and Get equal.
	now := s.now().UTC().Truncate(time.Microsecond)
	if !exists {
		d := Dashboard{ProjectID: projectID, UID: uid, Revision: 1, CreatedAt: now, UpdatedAt: now, Definition: norm}
		if err := s.store.Insert(ctx, d); err != nil {
			return Dashboard{}, false, err
		}
		return d, true, nil
	}

	same, err := sameDefinition(cur.Definition, norm)
	if err != nil {
		return Dashboard{}, false, err
	}
	if same {
		return cur, false, nil
	}
	d = cur
	d.Definition, d.Revision, d.UpdatedAt = norm, cur.Revision+1, now
	if err := s.store.Update(ctx, d, cur.Revision); err != nil {
		return Dashboard{}, false, err
	}
	return d, false, nil
}

// sameDefinition compares two normalized definitions by their JSON, which
// normalization makes canonical.
func sameDefinition(a, b Definition) (bool, error) {
	ja, err := json.Marshal(a)
	if err != nil {
		return false, fmt.Errorf("encode dashboard: %w", err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		return false, fmt.Errorf("encode dashboard: %w", err)
	}
	return bytes.Equal(ja, jb), nil
}

// Delete removes a dashboard, or returns ErrNotFound.
func (s *Service) Delete(ctx context.Context, projectID, uid string) error {
	return s.store.Delete(ctx, projectID, uid)
}
