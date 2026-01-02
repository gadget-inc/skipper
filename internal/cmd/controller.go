package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/function"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
	kubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned"
)

func NewController() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Start the controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			// flags have been parsed and validated by now, so don't print usage or errors anymore
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			// make klog use slog which will have already been configured by the root command
			klog.SetSlogLogger(slog.Default())

			config, err := rest.InClusterConfig()
			if errors.Is(err, rest.ErrNotInCluster) {
				config, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
			}
			if err != nil {
				return fmt.Errorf("failed to load kubernetes config: %w", err)
			}

			config.QPS = controller.FlagKubeConfigQPS.Value()
			config.Burst = controller.FlagKubeConfigBurst.Value()
			config.WrapTransport = func(rt http.RoundTripper) http.RoundTripper { return otelhttp.NewTransport(rt) }

			kubernetes, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			kubernetesMetrics, err := kubernetesmetrics.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create metrics client: %w", err)
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			ctrl := controller.New(controller.NewHTTPClient, kubernetes, kubernetesMetrics)
			if err := ctrl.Start(ctx); err != nil {
				return fmt.Errorf("failed to start controller: %w", err)
			}

			httpServer := &http.Server{
				Addr: net.JoinHostPort(controller.FlagHost.Value(), strconv.Itoa(controller.FlagPort.Value())),
				Handler: otelhttp.NewHandler(ctrl.Handler(), "",
					otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
					otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method + " " + r.URL.Path }),
				),
			}

			httpServerError := make(chan error, 1)

			go func() {
				log.Info(ctx, "serving controller", key.Addr.Slog(httpServer.Addr))
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					httpServerError <- err
				}
			}()

			select {
			case err := <-httpServerError:
				return fmt.Errorf("failed to serve controller: %w", err)
			case <-ctx.Done():
				log.Info(ctx, "shutting down controller")
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), controller.FlagShutdownTimeout.Value())
			defer cancel()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("failed to shutdown controller: %w", err)
			}

			log.Info(ctx, "controller shutdown")
			return nil
		},
	}

	controller.FlagAvailableReplicaDivisor.Bind(cmd)
	controller.FlagHPADownscaleStabilization.Bind(cmd)
	controller.FlagHPAInitialReadinessDelay.Bind(cmd)
	controller.FlagHPATolerance.Bind(cmd)
	controller.FlagHashRingWaitTime.Bind(cmd)
	controller.FlagHeartbeatTimeout.Bind(cmd)
	controller.FlagHost.Bind(cmd)
	controller.FlagKubeConfigBurst.Bind(cmd)
	controller.FlagKubeConfigQPS.Bind(cmd)
	controller.FlagNamespace.Bind(cmd)
	controller.FlagPasetoPrivateKey.Bind(cmd)
	controller.FlagPodIP.Bind(cmd)
	controller.FlagPort.Bind(cmd)
	controller.FlagScaleInterval.Bind(cmd)
	controller.FlagShutdownTimeout.Bind(cmd)
	function.FlagAssignPath.Bind(cmd)
	function.FlagAssignTimeout.Bind(cmd)
	function.FlagNamespaces.Bind(cmd)
	function.FlagSkipForbiddenNamespaces.Bind(cmd)

	return cmd
}
