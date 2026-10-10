package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/abogoyavlensky/tracelet/internal/project"
)

// ProjectStore keeps projects and tokens in the application database.
type ProjectStore struct {
	db *sql.DB
}

// NewProjectStore returns the store over db.
func NewProjectStore(db *sql.DB) *ProjectStore { return &ProjectStore{db: db} }

// Times are fixed-width UTC strings so they sort as text.
const timeLayout = "2006-01-02T15:04:05.000000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func formatNullTime(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(t), Valid: true}
}

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

func parseNullTime(s sql.NullString) (time.Time, error) {
	if !s.Valid {
		return time.Time{}, nil
	}
	return parseTime(s.String)
}

func isUnique(err error) bool {
	sqliteErr, ok := errors.AsType[*sqlite.Error](err)
	return ok && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

const insertProjectSQL = `INSERT INTO projects (id, slug, name, created_at) VALUES (?, ?, ?, ?)`

// InsertProject stores p, or returns project.ErrSlugTaken.
func (s *ProjectStore) InsertProject(ctx context.Context, p project.Project) error {
	_, err := s.db.ExecContext(ctx, insertProjectSQL, string(p.ID), p.Slug, p.Name, formatTime(p.CreatedAt))
	if isUnique(err) {
		return fmt.Errorf("insert project %s: %w", p.Slug, project.ErrSlugTaken)
	}
	if err != nil {
		return fmt.Errorf("insert project %s: %w", p.Slug, err)
	}
	return nil
}

const selectProjects = `SELECT id, slug, name, created_at FROM projects`

// ListProjects returns every project ordered by slug.
func (s *ProjectStore) ListProjects(ctx context.Context) ([]project.Project, error) {
	rows, err := s.db.QueryContext(ctx, selectProjects+" ORDER BY slug")
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var out []project.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectBySlug returns the project with slug, or project.ErrNotFound.
func (s *ProjectStore) ProjectBySlug(ctx context.Context, slug string) (project.Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, selectProjects+" WHERE slug = ?", slug))
	if errors.Is(err, sql.ErrNoRows) {
		return project.Project{}, project.ErrNotFound
	}
	return p, err
}

type scanner interface{ Scan(dest ...any) error }

func scanProject(row scanner) (project.Project, error) {
	var p project.Project
	var id, created string
	if err := row.Scan(&id, &p.Slug, &p.Name, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, err
		}
		return p, fmt.Errorf("scan project: %w", err)
	}
	p.ID = project.ID(id)
	var err error
	if p.CreatedAt, err = parseTime(created); err != nil {
		return p, fmt.Errorf("parse project created_at: %w", err)
	}
	return p, nil
}

const insertTokenSQL = `INSERT INTO tokens (id, project_id, scope, name, prefix, hash, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?)`

// InsertToken stores t with the hash of its plaintext.
func (s *ProjectStore) InsertToken(ctx context.Context, t project.Token, hash []byte) error {
	projectID := sql.NullString{String: string(t.ProjectID), Valid: t.ProjectID != ""}
	_, err := s.db.ExecContext(ctx, insertTokenSQL,
		string(t.ID), projectID, string(t.Scope), t.Name, t.Prefix, hash, formatTime(t.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

const selectTokens = `SELECT t.id, coalesce(t.project_id, ''), coalesce(p.slug, ''), t.scope, t.name, t.prefix,
  t.created_at, t.last_used_at, t.revoked_at, t.hash
  FROM tokens t LEFT JOIN projects p ON p.id = t.project_id`

// ListTokens returns every token, oldest first, without hashes.
func (s *ProjectStore) ListTokens(ctx context.Context) ([]project.Token, error) {
	rows, err := s.db.QueryContext(ctx, selectTokens+" ORDER BY t.created_at, t.id")
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	var out []project.Token
	for rows.Next() {
		t, _, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TokenByHash returns the token with hash, revoked or not, and its stored
// hash, or project.ErrNotFound.
func (s *ProjectStore) TokenByHash(ctx context.Context, hash []byte) (project.Token, []byte, error) {
	t, stored, err := scanToken(s.db.QueryRowContext(ctx, selectTokens+" WHERE t.hash = ?", hash))
	if errors.Is(err, sql.ErrNoRows) {
		return project.Token{}, nil, project.ErrNotFound
	}
	return t, stored, err
}

func scanToken(row scanner) (project.Token, []byte, error) {
	var t project.Token
	var id, projectID, scope, created string
	var lastUsed, revoked sql.NullString
	var hash []byte
	err := row.Scan(&id, &projectID, &t.ProjectSlug, &scope, &t.Name, &t.Prefix, &created, &lastUsed, &revoked, &hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return t, nil, err
		}
		return t, nil, fmt.Errorf("scan token: %w", err)
	}
	t.ID, t.ProjectID, t.Scope = project.TokenID(id), project.ID(projectID), project.Scope(scope)
	if t.CreatedAt, err = parseTime(created); err != nil {
		return t, nil, fmt.Errorf("parse token created_at: %w", err)
	}
	if t.LastUsedAt, err = parseNullTime(lastUsed); err != nil {
		return t, nil, fmt.Errorf("parse token last_used_at: %w", err)
	}
	if t.RevokedAt, err = parseNullTime(revoked); err != nil {
		return t, nil, fmt.Errorf("parse token revoked_at: %w", err)
	}
	return t, hash, nil
}

// TouchToken records that a token was used at at.
func (s *ProjectStore) TouchToken(ctx context.Context, id project.TokenID, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE tokens SET last_used_at = ? WHERE id = ?", formatTime(at), string(id)); err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

// RevokeToken marks a token revoked at at. Revoking a revoked token keeps
// the first time; an unknown ID is project.ErrNotFound.
func (s *ProjectStore) RevokeToken(ctx context.Context, id project.TokenID, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE tokens SET revoked_at = coalesce(revoked_at, ?) WHERE id = ?", formatNullTime(at), string(id))
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if n == 0 {
		return project.ErrNotFound
	}
	return nil
}

// CountActiveAdminTokens counts admin tokens that are not revoked.
func (s *ProjectStore) CountActiveAdminTokens(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM tokens WHERE scope = 'admin' AND revoked_at IS NULL").Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count admin tokens: %w", err)
	}
	return n, nil
}
