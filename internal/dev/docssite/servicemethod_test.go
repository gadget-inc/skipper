package docssite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRenderServiceMethod_KnownMethodsRenderRpcBlock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		request  string
		response string
	}{
		{"GetInstance", "GetInstanceRequest", "GetInstanceResponse"},
		{"Heartbeat", "HeartbeatRequest", "HeartbeatResponse"},
		{"Scale", "ScaleRequest", "ScaleResponse"},
		{"ReleaseInstance", "ReleaseInstanceRequest", "ReleaseInstanceResponse"},
		{"GetClusterState", "GetClusterStateRequest", "GetClusterStateResponse"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := renderServiceMethod(tc.name)
			assert.NilError(t, err)

			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			assert.Assert(t, len(lines) >= 3, "expected fenced block: %q", out)
			assert.Equal(t, lines[0], "```protobuf")
			want := "rpc " + tc.name + "(" + tc.request + ") returns (" + tc.response + ")"
			assert.Equal(t, lines[1], want)
			assert.Equal(t, lines[len(lines)-1], "```")
		})
	}
}

func TestRenderServiceMethod_UnknownMethodFails(t *testing.T) {
	t.Parallel()

	_, err := renderServiceMethod("Foo")
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, errServiceMethodUnknown))
	assert.ErrorContains(t, err, "Foo")
	for _, m := range []string{"GetInstance", "Heartbeat", "Scale", "ReleaseInstance", "GetClusterState"} {
		assert.Assert(t, strings.Contains(err.Error(), m),
			"error should list method %q: %v", m, err)
	}
}

// TestApiMdReferencesEveryControllerServiceMethod asserts every
// method on the ControllerService descriptor is referenced exactly
// once by a `{{< serviceMethod ... >}}` invocation in api.md. A
// future proto-level method addition without a doc update fails
// this test.
func TestApiMdReferencesEveryControllerServiceMethod(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../../docs/content/reference/api.md")
	assert.NilError(t, err)

	references := serviceMethodInvocationsRe.FindAllStringSubmatch(string(body), -1)
	got := map[string]int{}
	for _, m := range references {
		got[m[1]]++
	}

	want := map[string]int{
		"GetInstance":     1,
		"Heartbeat":       1,
		"Scale":           1,
		"ReleaseInstance": 1,
		"GetClusterState": 1,
	}
	assert.DeepEqual(t, got, want)
}

func TestBuild_ServiceMethodShortcodeRendersFencedBlock(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	outDir := t.TempDir()

	page := `---
title: Service
---

## GetInstance

{{< serviceMethod GetInstance >}}

## Heartbeat

{{< serviceMethod Heartbeat >}}
`
	assert.NilError(t, os.WriteFile(filepath.Join(srcDir, "svc.md"), []byte(page), 0o644))

	err := Build(srcDir, outDir, BuildOptions{})
	assert.NilError(t, err)

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "svc", "index.html"))
	assert.NilError(t, err)
	out := string(htmlBytes)

	// Chroma wraps `rpc` and `returns` in <span> elements; the
	// remaining method/message names render as plain text.
	for _, sig := range []string{
		"GetInstance(GetInstanceRequest)",
		"(GetInstanceResponse)",
		"Heartbeat(HeartbeatRequest)",
		"(HeartbeatResponse)",
	} {
		assert.Assert(t, strings.Contains(out, sig),
			"rendered HTML missing %q\n%s", sig, out)
	}
}
