package project_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/project"
)

func TestValidateSlug(t *testing.T) {
	for _, slug := range []string{"shop-2", "a", "0", strings.Repeat("a", 40)} {
		assert.NoError(t, project.ValidateSlug(slug), slug)
	}
	for _, slug := range []string{"Shop", "-a", strings.Repeat("a", 41), "", "a_b", "a b"} {
		var verr *project.ValidationError
		assert.ErrorAs(t, project.ValidateSlug(slug), &verr, slug)
	}
}

func TestNewToken(t *testing.T) {
	a, err := project.NewToken(rand.Reader)
	require.NoError(t, err)
	b, err := project.NewToken(rand.Reader)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(a.Plaintext, "tl_"))
	assert.Len(t, a.Plaintext, 43)
	sum := sha256.Sum256([]byte(a.Plaintext))
	assert.Equal(t, sum[:], a.Hash)
	assert.Equal(t, a.Plaintext[3:9], a.Prefix)
	assert.NotEqual(t, a.Plaintext, b.Plaintext)
}

func TestNewTokenShortRead(t *testing.T) {
	_, err := project.NewToken(bytes.NewReader([]byte{1, 2, 3}))
	require.Error(t, err)
}

func TestParseScope(t *testing.T) {
	s, err := project.ParseScope("read")
	require.NoError(t, err)
	assert.Equal(t, project.ScopeRead, s)
	_, err = project.ParseScope("write")
	require.Error(t, err)
}

func TestTokenAllows(t *testing.T) {
	admin := project.Token{Scope: project.ScopeAdmin}
	read := project.Token{Scope: project.ScopeRead}
	assert.True(t, admin.Allows(project.ScopeIngest))
	assert.True(t, read.Allows(project.ScopeRead))
	assert.False(t, read.Allows(project.ScopeIngest))
	assert.False(t, read.Allows(project.ScopeAdmin))
}
