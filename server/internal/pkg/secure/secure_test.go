package secure

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(h, "$argon2id$"))
	require.True(t, VerifyPassword("correct horse battery", h))
	require.False(t, VerifyPassword("wrong", h))
	require.False(t, VerifyPassword("x", "$bcrypt$garbage"))
}

func TestSealer(t *testing.T) {
	s, err := NewSealer("k")
	require.NoError(t, err)
	sealed, err := s.Seal("JBSWY3DPEHPK3PXP")
	require.NoError(t, err)
	plain, err := s.Open(sealed)
	require.NoError(t, err)
	require.Equal(t, "JBSWY3DPEHPK3PXP", plain)

	other, _ := NewSealer("other")
	_, err = other.Open(sealed)
	require.Error(t, err, "wrong key must not decrypt")
}

func TestToken(t *testing.T) {
	a, b := RandomToken("rkd_"), RandomToken("rkd_")
	require.NotEqual(t, a, b)
	require.Len(t, TokenHash(a), 64)
}
