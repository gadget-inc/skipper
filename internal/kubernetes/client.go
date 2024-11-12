package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/gadget-inc/fusion/internal/destination"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Client struct {
	clientset *kubernetes.Clientset
	ctx       context.Context
	cancel    context.CancelFunc
	listers   sync.Map
}

type Pod = apiv1.Pod

func NewClient(ctx context.Context) (*Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	// config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	config.QPS = 100
	config.Burst = 200

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	return &Client{
		clientset: clientset,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

func (k *Client) Close() {
	k.cancel()
}

func (k *Client) StartPodListeners(namespaces []string) {
	slog.InfoContext(k.ctx, "starting pod listeners", slog.Any("namespaces", namespaces))
	for _, namespace := range namespaces {
		factory := informers.NewSharedInformerFactoryWithOptions(
			k.clientset,
			5*time.Minute,
			informers.WithNamespace(namespace),
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.LabelSelector = "app.kubernetes.io/managed-by=fusion"
			}),
		)

		podInformer := factory.Core().V1().Pods()
		lister := podInformer.Lister()

		_, loaded := k.listers.LoadOrStore(namespace, lister)
		if !loaded {
			// start the informer
			go factory.Start(k.ctx.Done())

			// wait for the cache to sync
			if ok := cache.WaitForCacheSync(k.ctx.Done(), podInformer.Informer().HasSynced); !ok {
				slog.ErrorContext(k.ctx, "failed to sync informer cache", slog.String("namespace", namespace))
			}
		}
	}
}

func (k *Client) ListPods(ctx context.Context, namespace string, selector labels.Selector) ([]*apiv1.Pod, error) {
	listerAny, found := k.listers.Load(namespace)
	if !found {
		k.StartPodListeners([]string{namespace})
		listerAny, _ = k.listers.Load(namespace)
	}

	lister := listerAny.(listerv1.PodLister)
	return lister.List(selector)
}

func (k *Client) AssignPod(ctx context.Context, pod *Pod, dest destination.Destination) error {
	patchBody := `[{"op":"add","path":"/metadata/labels/fusion/environment-id","value":"` + dest.EnvironmentID + `"},{"op":"add","path":"/metadata/labels/fusion/status","value":"pending"}]`
	_, err := k.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{FieldManager: "fusion/router"})
	if err != nil {
		return fmt.Errorf("failed to assign pod: %w", err)
	}

	assignCtx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, "http://"+pod.Status.PodIP+":8080/assign", nil)
	if err != nil {
		return fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set("X-Fusion-Environment-Id", dest.EnvironmentID)
	req.Header.Set("X-Fusion-Deployment-Name", dest.DeploymentName)
	req.Header.Set("X-Fusion-Deployment-Namespace", dest.DeploymentNamespace)
	req.Header.Set("X-Fusion-Assignment-Secrets", dest.AssignmentSecrets)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send assign request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("assign request returned status %d", resp.StatusCode)
	}

	patchBody = `[{"op":"replace","path":"/metadata/labels/fusion/status","value":"ready"}]`
	_, err = k.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{FieldManager: "fusion/router"})
	if err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}

	return nil
}
