package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBanList(t *testing.T) {
	b := newBanList(3, time.Minute, time.Hour)
	require.False(t, b.fail("1.1.1.1"))
	require.False(t, b.fail("1.1.1.1"))
	require.False(t, b.isBanned("1.1.1.1"))
	require.True(t, b.fail("1.1.1.1"))
	require.True(t, b.isBanned("1.1.1.1"))
	require.False(t, b.isBanned("2.2.2.2"))
	require.Equal(t, 1, b.count())
}
