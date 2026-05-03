package krane_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"

	"github.com/gadget-inc/skipper/internal/dev/krane"
)

// kraneCorpusBaseBindings are the defaults applied to every corpus
// rendering. They mirror cmd/dev/kube_lint.go's kubeLintBaseBindings;
// if you add a new required base binding to the template, update both.
var kraneCorpusBaseBindings = map[string]any{
	"namespace":           "lint",
	"function_namespaces": []string{"default"},
	"image_tag":           "v1.0.0",
}

// kraneCorpusCases mirrors cmd/dev/kube_lint.go's kubeLintConfigs.
// Each case exercises a different branch through template.yaml.erb so
// the rendered YAML diffs flag any regression in either the krane
// shellout or the template itself.
var kraneCorpusCases = []struct {
	name     string
	bindings map[string]any
}{
	{name: "minimal", bindings: map[string]any{}},
	{
		name: "hpa-enabled",
		bindings: map[string]any{
			"controller_min_replicas": 2,
			"controller_max_replicas": 10,
			"router_min_replicas":     2,
			"router_max_replicas":     10,
		},
	},
	{
		name: "grpc-protocol",
		bindings: map[string]any{
			"controller_protocol": "grpc",
		},
	},
	{
		name: "nodeport",
		bindings: map[string]any{
			"router_node_port":         30080,
			"controller_node_port":     31021,
			"controller_web_node_port": 31022,
		},
	},
	{
		name: "inline-paseto",
		bindings: map[string]any{
			"unsafe_controller_paseto_private_key": "k4.secret.fake-paseto-key-for-linting",
		},
	},
	{
		name: "controller-affinity-override",
		bindings: map[string]any{
			"controller_affinity": map[string]any{
				"podAntiAffinity": map[string]any{
					"requiredDuringSchedulingIgnoredDuringExecution": []any{
						map[string]any{
							"topologyKey": "kubernetes.io/hostname",
							"labelSelector": map[string]any{
								"matchLabels": map[string]any{
									"app.kubernetes.io/name":      "skipper",
									"app.kubernetes.io/component": "controller",
								},
							},
						},
					},
				},
			},
		},
	},
	{
		name: "router-affinity-override",
		bindings: map[string]any{
			"router_affinity": map[string]any{
				"podAntiAffinity": map[string]any{
					"requiredDuringSchedulingIgnoredDuringExecution": []any{
						map[string]any{
							"topologyKey": "kubernetes.io/hostname",
							"labelSelector": map[string]any{
								"matchLabels": map[string]any{
									"app.kubernetes.io/name":      "skipper",
									"app.kubernetes.io/component": "router",
								},
							},
						},
					},
				},
			},
		},
	},
	{
		name: "combined-affinity",
		bindings: map[string]any{
			"controller_affinity": affinityCombined("controller"),
			"router_affinity":     affinityCombined("router"),
		},
	},
	{
		name: "full-features",
		bindings: map[string]any{
			"controller_annotations":   map[string]any{"prometheus.io/scrape": "true"},
			"router_annotations":       map[string]any{"prometheus.io/scrape": "true"},
			"controller_tolerations":   []any{map[string]any{"key": "dedicated", "operator": "Equal", "value": "skipper", "effect": "NoSchedule"}},
			"router_tolerations":       []any{map[string]any{"key": "dedicated", "operator": "Equal", "value": "skipper", "effect": "NoSchedule"}},
			"controller_node_selector": map[string]any{"node-pool": "skipper"},
			"router_node_selector":     map[string]any{"node-pool": "skipper"},
		},
	},
}

func affinityCombined(component string) map[string]any {
	return map[string]any{
		"nodeAffinity": map[string]any{
			"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{
				"nodeSelectorTerms": []any{
					map[string]any{
						"matchExpressions": []any{
							map[string]any{"key": "node-type", "operator": "In", "values": []string{"compute"}},
						},
					},
				},
			},
		},
		"podAntiAffinity": map[string]any{
			"preferredDuringSchedulingIgnoredDuringExecution": []any{
				map[string]any{
					"weight": 100,
					"podAffinityTerm": map[string]any{
						"topologyKey": "kubernetes.io/hostname",
						"labelSelector": map[string]any{
							"matchLabels": map[string]any{
								"app.kubernetes.io/name":      "skipper",
								"app.kubernetes.io/component": component,
							},
						},
					},
				},
			},
		},
	}
}

// TestRenderManifest_Corpus renders template.yaml.erb across the
// kube-lint corpus and diffs each rendering against a golden file.
// Update goldens with `go test ./internal/dev/krane/... -update`.
func TestRenderManifest_Corpus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping krane integration test in short mode")
	}

	templatePath := filepath.Join(repoRoot(t), "template.yaml.erb")

	for _, tc := range kraneCorpusCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			merged := map[string]any{}
			maps.Copy(merged, kraneCorpusBaseBindings)
			maps.Copy(merged, tc.bindings)

			out, err := krane.RenderManifest(context.Background(), templatePath, merged)
			assert.NilError(t, err)

			golden.Assert(t, string(out), tc.name+".golden.yaml")
		})
	}
}

// TestRender_ScaffoldsAndWrites confirms that Render copies any
// secrets.ejson into the render dir and writes rendered.yaml under
// tmp/krane/<namespace>/ relative to WORKSPACE_DIR.
func TestRender_ScaffoldsAndWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping krane integration test in short mode")
	}

	workspace := t.TempDir()
	t.Setenv("WORKSPACE_DIR", workspace)

	deployDir := filepath.Join(workspace, "deploy", "smoke")
	assert.NilError(t, os.MkdirAll(deployDir, 0o755))

	template := "kind: ConfigMap\nmetadata:\n  name: <%= name %>\n"
	assert.NilError(t, os.WriteFile(filepath.Join(deployDir, "configmap.yaml.erb"), []byte(template), 0o644))

	secrets := []byte(`{"_public_key":"abc"}`)
	assert.NilError(t, os.WriteFile(filepath.Join(deployDir, "secrets.ejson"), secrets, 0o644))

	renderDir, err := krane.Render(context.Background(), "smoke", map[string]any{"name": "smoke-cm"})
	assert.NilError(t, err)
	assert.Equal(t, renderDir, filepath.Join(workspace, "tmp", "krane", "smoke"))

	rendered, err := os.ReadFile(filepath.Join(renderDir, "rendered.yaml"))
	assert.NilError(t, err)
	assert.Assert(t, len(rendered) > 0, "rendered.yaml should not be empty")
	assert.Assert(t, strings.Contains(string(rendered), "name: smoke-cm"))

	copied, err := os.ReadFile(filepath.Join(renderDir, "secrets.ejson"))
	assert.NilError(t, err)
	assert.DeepEqual(t, copied, secrets)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("WORKSPACE_DIR"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	assert.NilError(t, err)
	// Tests run from the package directory; walk up until go.mod.
	for dir := wd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatalf("could not locate repo root from %s", wd)
	return ""
}
