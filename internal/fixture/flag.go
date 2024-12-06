package fixture

import (
	"testing"

	"github.com/gadget-inc/fusion/internal/flag"
)

func SetFlag[V any](t *testing.T, f *flag.Flag[V], value V) {
	original := f.Value
	t.Cleanup(func() { f.Value = original })
	f.Value = value
}
