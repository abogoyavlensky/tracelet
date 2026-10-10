// Package project holds projects and the scoped tokens that authenticate
// ingestion, queries, and administration.
package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Errors callers branch on.
var (
	ErrNotFound     = errors.New("not found")
	ErrSlugTaken    = errors.New("project slug already taken")
	ErrUnauthorized = errors.New("invalid or revoked token")
)

// ValidationError reports input that breaks a rule; its message is safe to
// show to the client.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ID is a project's immutable random ID, stored in every telemetry row.
type ID string

// TokenID identifies a token for listing and revocation.
type TokenID string

// Project is a namespace for telemetry. The slug is what users type; the ID
// is what telemetry stores, so a slug can change without touching data.
type Project struct {
	ID        ID
	Slug      string
	Name      string
	CreatedAt time.Time
}

// Scope is what a token may do.
type Scope string

// Scopes. An ingest token writes to one project; a read token reads one
// project or, without a project, all of them; an admin token does everything.
const (
	ScopeIngest Scope = "ingest"
	ScopeRead   Scope = "read"
	ScopeAdmin  Scope = "admin"
)

// ParseScope checks a scope name.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeIngest, ScopeRead, ScopeAdmin:
		return Scope(s), nil
	}
	return "", invalid("scope must be one of ingest, read, admin")
}

// Token is a token's metadata; the plaintext is never stored and the hash
// never leaves the store.
type Token struct {
	ID          TokenID
	ProjectID   ID     // empty for admin and all-project read tokens
	ProjectSlug string // empty when ProjectID is
	Scope       Scope
	Name        string
	Prefix      string
	CreatedAt   time.Time
	LastUsedAt  time.Time // zero when never used
	RevokedAt   time.Time // zero when active
}

// Allows reports whether the token grants scope. Admin grants everything.
func (t Token) Allows(scope Scope) bool {
	return t.Scope == ScopeAdmin || t.Scope == scope
}

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ValidateSlug checks a project slug: 1 to 40 lower-case letters, digits,
// and hyphens, not starting with a hyphen.
func ValidateSlug(slug string) error {
	if !slugRE.MatchString(slug) {
		return invalid("slug must be 1 to 40 lower-case letters, digits, and hyphens, starting with a letter or digit")
	}
	return nil
}

func validateName(field, name string) error {
	if strings.TrimSpace(name) == "" {
		return invalid("%s is required", field)
	}
	if len(name) > 200 {
		return invalid("%s must be at most 200 characters", field)
	}
	return nil
}

// TokenPrefix marks Tracelet tokens, so they are recognisable in configs and
// secret scanners.
const TokenPrefix = "tl_"

// Secret is a freshly generated token: the plaintext shown once, and what is
// stored instead of it.
type Secret struct {
	Plaintext string
	Hash      []byte
	Prefix    string
}

// NewToken generates tl_ plus 40 hex characters from r.
func NewToken(r io.Reader) (Secret, error) {
	b := make([]byte, 20)
	if _, err := io.ReadFull(r, b); err != nil {
		return Secret{}, fmt.Errorf("generate token: %w", err)
	}
	return secretFor(TokenPrefix + hex.EncodeToString(b)), nil
}

func secretFor(plaintext string) Secret {
	body := strings.TrimPrefix(plaintext, TokenPrefix)
	return Secret{Plaintext: plaintext, Hash: HashToken(plaintext), Prefix: body[:min(6, len(body))]}
}

// HashToken returns the SHA-256 the store keeps for a token. Tokens are 160
// random bits, so a fast unsalted hash is enough.
func HashToken(plaintext string) []byte {
	h := sha256.Sum256([]byte(plaintext))
	return h[:]
}

func newID(r io.Reader) (string, error) {
	b := make([]byte, 8)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
