package project

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Store persists projects and tokens. Lookups that find nothing return
// ErrNotFound; InsertProject returns ErrSlugTaken for a duplicate slug.
type Store interface {
	InsertProject(ctx context.Context, p Project) error
	ListProjects(ctx context.Context) ([]Project, error)
	ProjectBySlug(ctx context.Context, slug string) (Project, error)
	InsertToken(ctx context.Context, t Token, hash []byte) error
	ListTokens(ctx context.Context) ([]Token, error)
	TokenByHash(ctx context.Context, hash []byte) (Token, []byte, error)
	TouchToken(ctx context.Context, id TokenID, at time.Time) error
	RevokeToken(ctx context.Context, id TokenID, at time.Time) error
	CountActiveAdminTokens(ctx context.Context) (int, error)
}

// touchEvery limits last_used_at writes to one per token per minute, so an
// ingest token used several times a second does not write on every request.
const touchEvery = time.Minute

// Service manages projects and tokens.
type Service struct {
	store Store
	now   func() time.Time
	rand  io.Reader
}

// NewService returns a service over store, with now and rand as the clock
// and the source of IDs and tokens.
func NewService(store Store, now func() time.Time, rand io.Reader) *Service {
	return &Service{store: store, now: now, rand: rand}
}

// CreateProject creates a project with a fresh ID.
func (s *Service) CreateProject(ctx context.Context, slug, name string) (Project, error) {
	if err := ValidateSlug(slug); err != nil {
		return Project{}, err
	}
	if name == "" {
		name = slug
	}
	if err := validateName("name", name); err != nil {
		return Project{}, err
	}
	id, err := newID(s.rand)
	if err != nil {
		return Project{}, err
	}
	p := Project{ID: ID(id), Slug: slug, Name: name, CreatedAt: s.now().UTC()}
	if err := s.store.InsertProject(ctx, p); err != nil {
		return Project{}, err
	}
	return p, nil
}

// ListProjects returns every project, by slug.
func (s *Service) ListProjects(ctx context.Context) ([]Project, error) {
	return s.store.ListProjects(ctx)
}

// FindBySlug returns the project with slug, or ErrNotFound.
func (s *Service) FindBySlug(ctx context.Context, slug string) (Project, error) {
	return s.store.ProjectBySlug(ctx, slug)
}

// NewTokenParams describes a token to create.
type NewTokenParams struct {
	ProjectSlug string // required for ingest, optional for read, empty for admin
	Scope       Scope
	Name        string
}

// CreateToken creates a token and returns its metadata and the plaintext,
// which is not retrievable afterwards.
func (s *Service) CreateToken(ctx context.Context, p NewTokenParams) (Token, string, error) {
	if _, err := ParseScope(string(p.Scope)); err != nil {
		return Token{}, "", err
	}
	if err := validateName("name", p.Name); err != nil {
		return Token{}, "", err
	}
	t := Token{Scope: p.Scope, Name: p.Name, CreatedAt: s.now().UTC()}
	switch {
	case p.Scope == ScopeIngest && p.ProjectSlug == "":
		return Token{}, "", invalid("an ingest token needs a project")
	case p.Scope == ScopeAdmin && p.ProjectSlug != "":
		return Token{}, "", invalid("an admin token cannot be limited to a project")
	case p.ProjectSlug != "":
		proj, err := s.store.ProjectBySlug(ctx, p.ProjectSlug)
		if err != nil {
			return Token{}, "", fmt.Errorf("project %s: %w", p.ProjectSlug, err)
		}
		t.ProjectID, t.ProjectSlug = proj.ID, proj.Slug
	}

	secret, err := NewToken(s.rand)
	if err != nil {
		return Token{}, "", err
	}
	if err := s.insertToken(ctx, &t, secret); err != nil {
		return Token{}, "", err
	}
	return t, secret.Plaintext, nil
}

func (s *Service) insertToken(ctx context.Context, t *Token, secret Secret) error {
	id, err := newID(s.rand)
	if err != nil {
		return err
	}
	t.ID, t.Prefix = TokenID(id), secret.Prefix
	return s.store.InsertToken(ctx, *t, secret.Hash)
}

// ListTokens returns every token's metadata, never hashes or plaintexts.
func (s *Service) ListTokens(ctx context.Context) ([]Token, error) {
	return s.store.ListTokens(ctx)
}

// RevokeToken revokes a token; it returns ErrNotFound for an unknown ID.
func (s *Service) RevokeToken(ctx context.Context, id TokenID) error {
	return s.store.RevokeToken(ctx, id, s.now().UTC())
}

// dummyHash stands in for a stored hash when the token is unknown, so known
// and unknown tokens take the same comparison.
var dummyHash = make([]byte, 32)

// Authenticate returns the active token whose plaintext this is, or
// ErrUnauthorized. Lookup is by hash, so the plaintext is never compared
// directly, and the stored hash is compared in constant time.
func (s *Service) Authenticate(ctx context.Context, plaintext string) (Token, error) {
	hash := HashToken(plaintext)
	t, stored, err := s.store.TokenByHash(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		stored = dummyHash
	} else if err != nil {
		return Token{}, fmt.Errorf("authenticate: %w", err)
	}
	if subtle.ConstantTimeCompare(stored, hash) != 1 || !strings.HasPrefix(plaintext, TokenPrefix) {
		return Token{}, ErrUnauthorized
	}
	if !t.RevokedAt.IsZero() {
		return Token{}, ErrUnauthorized
	}

	now := s.now().UTC()
	if now.Sub(t.LastUsedAt) >= touchEvery {
		if err := s.store.TouchToken(ctx, t.ID, now); err != nil {
			return Token{}, fmt.Errorf("authenticate: %w", err)
		}
		t.LastUsedAt = now
	}
	return t, nil
}

// BootstrapTokenName names the admin token created on first start.
const BootstrapTokenName = "bootstrap admin"

// Bootstrap makes sure an admin token exists. With none, it creates one: from
// seed when given, returning "", or random, returning the plaintext to show
// once. With an active admin token present it does nothing and returns "".
func (s *Service) Bootstrap(ctx context.Context, seed string) (string, error) {
	n, err := s.store.CountActiveAdminTokens(ctx)
	if err != nil {
		return "", fmt.Errorf("bootstrap: %w", err)
	}
	if n > 0 {
		return "", nil
	}

	var secret Secret
	if seed != "" {
		if !strings.HasPrefix(seed, TokenPrefix) || len(seed) < len(TokenPrefix)+16 {
			return "", invalid("TRACELET_ADMIN_TOKEN must start with %s and have at least 16 characters after it", TokenPrefix)
		}
		secret = secretFor(seed)
	} else {
		secret, err = NewToken(s.rand)
		if err != nil {
			return "", err
		}
	}

	t := Token{Scope: ScopeAdmin, Name: BootstrapTokenName, CreatedAt: s.now().UTC()}
	if err := s.insertToken(ctx, &t, secret); err != nil {
		return "", fmt.Errorf("bootstrap: %w", err)
	}
	if seed != "" {
		return "", nil
	}
	return secret.Plaintext, nil
}
