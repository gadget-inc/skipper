package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	osexec "os/exec"

	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/devenv"
	"github.com/gadget-inc/skipper/internal/dev/krane"
	"github.com/spf13/cobra"
)

type bindingConfig struct {
	name     string
	bindings map[string]any
}

var kubeLintBaseBindings = map[string]any{
	"namespace":           "lint",
	"function_namespaces": []string{"default"},
	"image_tag":           "v1.0.0",
}

var kubeLintConfigs = []bindingConfig{
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

// newKubeLintCmd renders template.yaml.erb across the binding-config
// corpus via internal/dev/krane and runs yamllint + kube-linter on
// each rendered manifest.
func newKubeLintCmd() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "kube-lint",
		Short: "Render template.yaml.erb across binding configurations and lint each",
		RunE: func(c *cobra.Command, args []string) error {
			return runKubeLint(c.Context())
		},
	})
}

func runKubeLint(ctx context.Context) error {
	root := devenv.RepoRoot()
	lintDir := filepath.Join(root, "tmp", "kube-lint")
	if err := krane.EmptyDir(lintDir); err != nil {
		return err
	}

	templatePath := filepath.Join(root, "template.yaml.erb")
	yamllintConfig := filepath.Join(root, ".yamllint.yaml")

	hasErrors := false
	for _, cfg := range kubeLintConfigs {
		configDir := filepath.Join(lintDir, cfg.name)
		if err := krane.EmptyDir(configDir); err != nil {
			return err
		}

		merged := map[string]any{}
		maps.Copy(merged, kubeLintBaseBindings)
		maps.Copy(merged, cfg.bindings)

		rendered, err := krane.RenderManifest(ctx, templatePath, merged)
		if err != nil {
			return fmt.Errorf("krane render %s: %w", cfg.name, err)
		}

		manifest := filepath.Join(configDir, "template.yaml")
		if err := os.WriteFile(manifest, rendered, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", manifest, err)
		}

		if err := runYamllint(ctx, yamllintConfig, manifest); err != nil {
			hasErrors = true
		}
		if err := runKubeLinter(ctx, root, manifest); err != nil {
			hasErrors = true
		}
	}

	if hasErrors {
		return errors.New("kube-lint reported issues")
	}

	fmt.Printf("✓ %d template configurations linted successfully\n", len(kubeLintConfigs))
	return nil
}

var yamllintParsableLine = regexp.MustCompile(`^(.+?:\d+:\d+): (\[error\]) (.+)$`)

func runYamllint(ctx context.Context, configPath, manifest string) error {
	var stdout, stderr bytes.Buffer
	cmd := osexec.CommandContext(ctx, "yamllint", "-f", "parsable", "-c", configPath, manifest)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	output := strings.TrimSpace(stdout.String())
	if output == "" {
		output = strings.TrimSpace(stderr.String())
	}
	for line := range strings.SplitSeq(output, "\n") {
		if line == "" {
			continue
		}
		if m := yamllintParsableLine.FindStringSubmatch(line); m != nil {
			fmt.Fprintf(os.Stderr, "%s: %s %s\n", m[1], m[2], m[3])
		} else {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	return err
}

type kubeLinterReport struct {
	Reports []struct {
		Object struct {
			Metadata struct {
				FilePath string `json:"FilePath"`
			} `json:"Metadata"`
			K8sObject struct {
				Name string `json:"Name"`
			} `json:"K8sObject"`
		} `json:"Object"`
		Check      string `json:"Check"`
		Diagnostic struct {
			Message string `json:"Message"`
		} `json:"Diagnostic"`
	} `json:"Reports"`
}

func runKubeLinter(ctx context.Context, root, manifest string) error {
	var stdout, stderr bytes.Buffer
	cmd := osexec.CommandContext(ctx, "kube-linter", "lint", "--format", "json", manifest)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var report kubeLinterReport
	if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
		raw := strings.TrimSpace(stdout.String())
		if raw == "" {
			raw = strings.TrimSpace(stderr.String())
		}
		fmt.Fprintln(os.Stderr, raw)
		return err
	}
	basePath := root
	if !strings.HasSuffix(basePath, string(os.PathSeparator)) {
		basePath += string(os.PathSeparator)
	}
	for _, r := range report.Reports {
		file := r.Object.Metadata.FilePath
		if file == "" {
			file = manifest
		}
		file = strings.TrimPrefix(file, basePath)
		objectName := r.Object.K8sObject.Name
		if objectName == "" {
			objectName = "unknown"
		}
		check := r.Check
		if check == "" {
			check = "unknown"
		}
		message := r.Diagnostic.Message
		if message == "" {
			message = "unknown error"
		}
		fmt.Fprintf(os.Stderr, "%s: %s [%s]: %s\n", file, objectName, check, message)
	}
	return err
}
