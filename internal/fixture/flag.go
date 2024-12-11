package fixture

import (
	"testing"

	"github.com/gadget-inc/fusion/internal/flag"
	"github.com/stretchr/testify/require"
)

func SetFlag[V any](t *testing.T, f *flag.Flag[V], value V) {
	f.Init()
	original := f.Value()
	t.Cleanup(func() { require.NoError(t, f.SetValue(original)) })
	require.NoError(t, f.SetValue(value))
}
