package fixture

import (
	"testing"

	"github.com/gadget-inc/skipper/internal/flag"
	"gotest.tools/v3/assert"
)

func SetFlag[V any](t *testing.T, f *flag.Flag[V], value V) {
	f.Init()
	original := f.Value()
	t.Cleanup(func() { assert.NilError(t, f.SetValue(original)) })
	assert.NilError(t, f.SetValue(value))
}
