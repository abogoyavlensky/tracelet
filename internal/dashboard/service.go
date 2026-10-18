package dashboard

import "context"

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
