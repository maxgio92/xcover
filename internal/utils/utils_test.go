package utils_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/utrace/internal/utils"
)

func TestHash(t *testing.T) {
	require.NotEqual(t, utils.Hash("foo"), utils.Hash("bar"),
		"Hash should differ for different inputs",
	)

	require.Equal(
		t, utils.Hash("baz"), utils.Hash("baz"),
		"Hash should be deterministic for the same input",
	)
}

func TestLenSyncMap(t *testing.T) {
	var m sync.Map
	require.Equal(t, 0, utils.LenSyncMap(&m))

	m.Store("foo", 1)
	m.Store("bar", 2)
	m.Store("baz", 3)

	require.Equal(t, 3, utils.LenSyncMap(&m))
}
