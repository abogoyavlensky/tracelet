package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/abogoyavlensky/tracelet/internal/dashboard"
)

// DashboardStore keeps dashboards in the application database.
type DashboardStore struct {
	db *sql.DB
}

// NewDashboardStore returns the store over db.
func NewDashboardStore(db *sql.DB) *DashboardStore { return &DashboardStore{db: db} }

// isPrimaryKey reports a primary key violation, which SQLite reports apart
// from a unique one for a table whose key is not its rowid.
func isPrimaryKey(err error) bool {
	sqliteErr, ok := errors.AsType[*sqlite.Error](err)
	return ok && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

const selectDashboards = `SELECT project_id, uid, definition, revision, created_at, updated_at FROM dashboards`

// List returns a project's dashboards ordered by title, then uid.
func (s *DashboardStore) List(ctx context.Context, projectID string) ([]dashboard.Dashboard, error) {
	rows, err := s.db.QueryContext(ctx, selectDashboards+" WHERE project_id = ?", projectID)
	if err != nil {
		return nil, fmt.Errorf("list dashboards: %w", err)
	}
	defer rows.Close()

	var out []dashboard.Dashboard
	for rows.Next() {
		d, err := scanDashboard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list dashboards: %w", err)
	}
	// The title lives in the JSON, so the order is applied here.
	slices.SortFunc(out, func(a, b dashboard.Dashboard) int {
		return cmp.Or(cmp.Compare(a.Title, b.Title), cmp.Compare(a.UID, b.UID))
	})
	return out, nil
}

// Get returns one dashboard, or dashboard.ErrNotFound.
func (s *DashboardStore) Get(ctx context.Context, projectID, uid string) (dashboard.Dashboard, error) {
	d, err := scanDashboard(s.db.QueryRowContext(ctx, selectDashboards+" WHERE project_id = ? AND uid = ?", projectID, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return dashboard.Dashboard{}, dashboard.ErrNotFound
	}
	return d, err
}

func scanDashboard(row scanner) (dashboard.Dashboard, error) {
	var d dashboard.Dashboard
	var def, created, updated string
	if err := row.Scan(&d.ProjectID, &d.UID, &def, &d.Revision, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return d, err
		}
		return d, fmt.Errorf("scan dashboard: %w", err)
	}
	if err := json.Unmarshal([]byte(def), &d.Definition); err != nil {
		return d, fmt.Errorf("decode dashboard %s: %w", d.UID, err)
	}
	var err error
	if d.CreatedAt, err = parseTime(created); err != nil {
		return d, fmt.Errorf("parse dashboard created_at: %w", err)
	}
	if d.UpdatedAt, err = parseTime(updated); err != nil {
		return d, fmt.Errorf("parse dashboard updated_at: %w", err)
	}
	return d, nil
}

const insertDashboardSQL = `INSERT INTO dashboards (project_id, uid, definition, revision, created_at, updated_at)
  VALUES (?, ?, ?, ?, ?, ?)`

// Insert stores a new dashboard, or returns a dashboard.ConflictError when
// the project already has one with its uid.
func (s *DashboardStore) Insert(ctx context.Context, d dashboard.Dashboard) error {
	def, err := json.Marshal(d.Definition)
	if err != nil {
		return fmt.Errorf("encode dashboard %s: %w", d.UID, err)
	}
	_, err = s.db.ExecContext(ctx, insertDashboardSQL,
		d.ProjectID, d.UID, string(def), d.Revision, formatTime(d.CreatedAt), formatTime(d.UpdatedAt))
	if isPrimaryKey(err) || isUnique(err) {
		return &dashboard.ConflictError{Message: "dashboard " + d.UID + " already exists"}
	}
	if err != nil {
		return fmt.Errorf("insert dashboard %s: %w", d.UID, err)
	}
	return nil
}

const updateDashboardSQL = `UPDATE dashboards SET definition = ?, revision = ?, updated_at = ?
  WHERE project_id = ? AND uid = ? AND revision = ?`

// Update replaces a dashboard if it is still at prevRevision, or returns a
// dashboard.ConflictError: a compare-and-swap, so two writers cannot both
// win.
func (s *DashboardStore) Update(ctx context.Context, d dashboard.Dashboard, prevRevision int) error {
	def, err := json.Marshal(d.Definition)
	if err != nil {
		return fmt.Errorf("encode dashboard %s: %w", d.UID, err)
	}
	res, err := s.db.ExecContext(ctx, updateDashboardSQL,
		string(def), d.Revision, formatTime(d.UpdatedAt), d.ProjectID, d.UID, prevRevision)
	if err != nil {
		return fmt.Errorf("update dashboard %s: %w", d.UID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update dashboard %s: %w", d.UID, err)
	}
	if n == 0 {
		return &dashboard.ConflictError{Message: "dashboard " + d.UID + " changed while saving; reload it and try again"}
	}
	return nil
}

// Delete removes a dashboard, or returns dashboard.ErrNotFound.
func (s *DashboardStore) Delete(ctx context.Context, projectID, uid string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM dashboards WHERE project_id = ? AND uid = ?", projectID, uid)
	if err != nil {
		return fmt.Errorf("delete dashboard %s: %w", uid, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete dashboard %s: %w", uid, err)
	}
	if n == 0 {
		return dashboard.ErrNotFound
	}
	return nil
}
