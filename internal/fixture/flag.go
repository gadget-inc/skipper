package fixture

import (
	"testing"

	"github.com/gadget-inc/fusion/internal/flag"
	"github.com/shoenig/test/must"
)

func SetFlag[V any](t *testing.T, f *flag.Flag[V], value V) {
	f.Init()
	original := f.Value()
	t.Cleanup(func() { must.NoError(t, f.SetValue(original)) })
	must.NoError(t, f.SetValue(value))
}
