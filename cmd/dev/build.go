package main

import (
	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/devenv"
	"github.com/gadget-inc/skipper/internal/dev/dockerimg"
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newBuildCmd builds the controller / router / fixture container
// images: docker buildx bake for the controller / router targets,
// docker buildx build for the fixture container, and an optional
// `kind load` step for CI.
func newBuildCmd() *cobra.Command {
	var (
		registry   string
		tag        string
		platform   string
		only       string
		kindLoad   bool
		load       bool
		push       bool
		provenance bool
	)

	return cmd.Build(cmd.Spec{
		Use:   "build [flags]",
		Short: "Build the controller, router, and fixture images",
		Flags: func(fs *pflag.FlagSet) {
			fs.StringVar(&registry, "registry", "", "Registry of the image")
			fs.StringVar(&tag, "tag", "", "Build tag (default: sha-<git>-<arch>)")
			fs.StringVar(&platform, "platform", "", "Platform to build for (default: <os>/<arch> from docker)")
			fs.StringVar(&only, "only", "controller,router,fixtures", "Only build the given images")
			fs.BoolVar(&kindLoad, "kind", devenv.IsCI(), "Load images into the kind cluster after build")
			fs.BoolVar(&load, "load", true, "Load the image into the local docker daemon")
			fs.BoolVar(&push, "push", false, "Push the image to the registry")
			fs.BoolVar(&provenance, "provenance", false, "Enable build provenance")
		},
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()

			if tag == "" {
				t, err := dockerimg.Tag(ctx)
				if err != nil {
					return err
				}
				tag = t
			}
			if platform == "" {
				p, err := dockerimg.Platform(ctx)
				if err != nil {
					return err
				}
				platform = p
			}

			components := devenv.ExpandSkipperAlias(only)
			buildController := components["controller"]
			buildRouter := components["router"]

			if buildController || buildRouter {
				targets := []string{}
				if buildController {
					targets = append(targets, "controller")
				}
				if buildRouter {
					targets = append(targets, "router")
				}

				bakeFlags := []string{}
				if load {
					bakeFlags = append(bakeFlags, "--load")
				}
				if push {
					bakeFlags = append(bakeFlags, "--push")
				}
				if !provenance {
					bakeFlags = append(bakeFlags, "--provenance=false")
				}

				env := []string{"TAG=" + tag, "PLATFORM=" + platform}
				if registry != "" {
					env = append(env, "REGISTRY="+registry)
				}

				args := append([]string{"buildx", "bake"}, targets...)
				args = append(args, bakeFlags...)
				if err := exec.Run(ctx, "docker", args, exec.Env(env...)); err != nil {
					return err
				}

				if kindLoad {
					if err := devenv.KindLoadImage(ctx, "skipper-controller", registry, tag, buildController); err != nil {
						return err
					}
					if err := devenv.KindLoadImage(ctx, "skipper-router", registry, tag, buildRouter); err != nil {
						return err
					}
				}
			}

			if components["fixtures"] {
				imageName := "skipper-fixtures-fixture"
				buildFlags := []string{
					"buildx", "build", ".",
					"--file=fixture/Dockerfile",
					"--platform=" + platform,
					"--tag=" + imageName + ":" + tag,
				}
				if load {
					buildFlags = append(buildFlags, "--load")
				}
				if !provenance {
					buildFlags = append(buildFlags, "--provenance=false")
				}
				if err := exec.Run(ctx, "docker", buildFlags); err != nil {
					return err
				}

				if kindLoad {
					if err := exec.Run(ctx, "kind", []string{"load", "docker-image", imageName + ":" + tag}); err != nil {
						return err
					}
				}
			}

			return nil
		},
	})
}
