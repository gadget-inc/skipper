package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gadget-inc/skipper/internal/dev/dockerimg"
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/gadget-inc/skipper/internal/dev/krane"
	"github.com/spf13/cobra"
)

// newDeployCmd builds the requested images, optionally regenerates the
// PASETO keypair under tmp/paseto/, and walks the --only set to render
// and apply each component's namespace via internal/dev/krane.
func newDeployCmd() *cobra.Command {
	var (
		build                 bool
		generatePasetoKeypair bool
		development           bool
		only                  string
		otel                  bool
		test                  bool
	)

	ci := isCI()

	cmd := &cobra.Command{
		Use:   "deploy [flags]",
		Short: "Build and deploy Skipper components to Kubernetes",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if os.Getenv("SKIPPER_KUBECTL_CONTEXT") == "" {
				os.Setenv("SKIPPER_KUBECTL_CONTEXT", "orbstack")
			}

			components := expandSkipperAlias(only)

			// --generate-paseto-keypair defaults to true when the
			// keypair is missing on disk, false when present; an
			// explicit flag value wins.
			if !cmd.Flags().Changed("generate-paseto-keypair") {
				generatePasetoKeypair = !pasetoKeypairExists()
			}

			if build {
				if err := exec.Run(ctx, "go", []string{"run", "./cmd/dev", "build"}); err != nil {
					return err
				}
			}

			imageTag, err := dockerimg.Tag(ctx)
			if err != nil {
				return err
			}
			controllerImageID, _ := dockerimg.ImageID(ctx, "skipper-controller")
			routerImageID, _ := dockerimg.ImageID(ctx, "skipper-router")

			if generatePasetoKeypair {
				if err := generatePasetoKeypairFiles(); err != nil {
					return err
				}
			}

			if components["metrics-server"] {
				if err := deployMetricsServer(ctx); err != nil {
					return err
				}
			}

			if components["otel-lgtm"] && otel {
				if err := krane.Deploy(ctx, "otel-lgtm", nil); err != nil {
					return err
				}
			}

			if components["fixtures"] {
				fixtureBindings := func() map[string]any {
					return map[string]any{
						"fixture_image_tag": imageTag,
						"env": map[string]any{
							"OTEL_DENO":                   otel,
							"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
							"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
						},
					}
				}
				if development {
					if err := krane.Deploy(ctx, "skipper-development-fixtures", fixtureBindings()); err != nil {
						return err
					}
				}
				if test {
					if err := krane.Deploy(ctx, "skipper-test-fixtures", fixtureBindings()); err != nil {
						return err
					}
				}
			}

			if components["controller"] || components["router"] {
				root := repoRoot()
				pasetoPrivate, err := os.ReadFile(filepath.Join(root, "tmp", "paseto", "private.pem"))
				if err != nil {
					return fmt.Errorf("read paseto private key: %w", err)
				}

				envBindings := map[string]any{
					"SKIPPER_TELEMETRY":           otel,
					"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
					"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
				}

				if development {
					bindings := map[string]any{
						"controller_image_name":                "skipper-controller",
						"router_image_name":                    "skipper-router",
						"image_tag":                            imageTag,
						"controller_annotations":               map[string]any{"skipper/image-id": controllerImageID},
						"router_annotations":                   map[string]any{"skipper/image-id": routerImageID},
						"namespace":                            "skipper-development",
						"function_namespaces":                  []string{"skipper-development-fixtures"},
						"unsafe_controller_paseto_private_key": string(pasetoPrivate),
						"router_node_port":                     31020,
						"controller_node_port":                 31021,
						"controller_web_node_port":             31022,
						"controller_web_template_host_dir":     filepath.Join(root, "internal", "web"),
						"env":                                  envBindings,
					}
					if err := krane.Deploy(ctx, "skipper-development", bindings); err != nil {
						return err
					}
				}

				if test {
					bindings := map[string]any{
						"controller_image_name":                "skipper-controller",
						"router_image_name":                    "skipper-router",
						"image_tag":                            imageTag,
						"controller_annotations":               map[string]any{"skipper/image-id": controllerImageID},
						"router_annotations":                   map[string]any{"skipper/image-id": routerImageID},
						"namespace":                            "skipper-test",
						"function_namespaces":                  []string{"skipper-test-fixtures"},
						"unsafe_controller_paseto_private_key": string(pasetoPrivate),
						"router_node_port":                     31030,
						"controller_node_port":                 31031,
						"controller_web_node_port":             31032,
						"env":                                  envBindings,
					}
					if err := krane.Deploy(ctx, "skipper-test", bindings); err != nil {
						return err
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&build, "build", true, "Build images before deploying")
	cmd.Flags().BoolVar(&generatePasetoKeypair, "generate-paseto-keypair", false,
		"Generate a paseto keypair (defaults to true when tmp/paseto/{public,private}.pem are missing)")
	cmd.Flags().BoolVar(&development, "development", !ci, "Deploy development namespaces")
	cmd.Flags().StringVar(&only, "only", "controller,router,fixtures,metrics-server,otel-lgtm", "Comma-separated components to deploy")
	cmd.Flags().BoolVar(&otel, "otel", !ci, "Enable OpenTelemetry")
	cmd.Flags().BoolVar(&test, "test", true, "Deploy test namespaces")

	return cmd
}

func pasetoKeypairExists() bool {
	root := repoRoot()
	_, e1 := os.Stat(filepath.Join(root, "tmp", "paseto", "private.pem"))
	_, e2 := os.Stat(filepath.Join(root, "tmp", "paseto", "public.pem"))
	return e1 == nil && e2 == nil
}

// generatePasetoKeypairFiles regenerates an ed25519 keypair under
// tmp/paseto/, replacing any existing files.
func generatePasetoKeypairFiles() error {
	root := repoRoot()
	dir := filepath.Join(root, "tmp", "paseto")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ed25519 keypair: %w", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("encode private key as PKCS8: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(filepath.Join(dir, "private.pem"), privPEM, 0o600); err != nil {
		return err
	}

	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return fmt.Errorf("encode public key as SPKI: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(filepath.Join(dir, "public.pem"), pubPEM, 0o644); err != nil {
		return err
	}

	return listDir(dir)
}

func listDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return err
		}
		fmt.Printf("%s\t%d\t%s\n", info.Mode(), info.Size(), filepath.Join(dir, e.Name()))
	}
	return nil
}

// deployMetricsServer applies the cluster-scoped resources (via krane
// global-deploy) and the kube-system namespace overrides used to swap
// the bundled metrics-server for a Kind-friendly build.
func deployMetricsServer(ctx context.Context) error {
	clusterRenderDir, err := krane.Render(ctx, "cluster", nil)
	if err != nil {
		return err
	}
	clusterFiles, err := manifestFiles(clusterRenderDir)
	if err != nil {
		return err
	}
	args := []string{"global-deploy", os.Getenv("SKIPPER_KUBECTL_CONTEXT")}
	for _, f := range clusterFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "--selector=app.kubernetes.io/managed-by=krane")
	if err := exec.Run(ctx, "krane", args); err != nil {
		return err
	}

	kubeSystemRenderDir, err := krane.Render(ctx, "kube-system", nil)
	if err != nil {
		return err
	}
	kubeSystemFiles, err := manifestFiles(kubeSystemRenderDir)
	if err != nil {
		return err
	}
	args = []string{"deploy", "kube-system", os.Getenv("SKIPPER_KUBECTL_CONTEXT")}
	for _, f := range kubeSystemFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "--selector=app.kubernetes.io/managed-by=krane", "--protected-namespaces=default", "kube-public")
	return exec.Run(ctx, "krane", args)
}

func manifestFiles(renderDir string) ([]string, error) {
	entries, err := os.ReadDir(renderDir)
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, filepath.Join(renderDir, e.Name()))
	}
	return files, nil
}
