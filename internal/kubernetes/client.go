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
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	labelTenant     = "fusion/tenant"
	labelDeployment = "fusion/deployment"
	labelStatus     = "fusion/status"

	patchLabelTenant = "fusion~1tenant"
	patchLabelStatus = "fusion~1status"
)

type Client struct {
	clientset *kubernetes.Clientset
	ctx       context.Context
	cancel    context.CancelFunc
	listers   sync.Map
}

func NewClient(ctx context.Context) (*Client, error) {
	config, err := rest.InClusterConfig()
	if errors.Is(err, rest.ErrNotInCluster) {
		config, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	}
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

func (k *Client) ListAssignedPods(ctx context.Context, dest destination.Destination) ([]*Pod, error) {
	return k.listPods(dest.Namespace, labels.SelectorFromSet(labels.Set{
		labelTenant:     dest.Tenant,
		labelDeployment: dest.Deployment,
		labelStatus:     "ready",
	}))
}

func (k *Client) ListAvailablePods(ctx context.Context, dest destination.Destination) ([]*Pod, error) {
	noTenant, err := labels.NewRequirement(labelTenant, selection.DoesNotExist, nil)
	if err != nil {
		return nil, err
	}

	equalDeploymentName, err := labels.NewRequirement(labelDeployment, selection.Equals, []string{dest.Deployment})
	if err != nil {
		return nil, err
	}

	pods, err := k.listPods(dest.Namespace, labels.NewSelector().Add(*noTenant, *equalDeploymentName))
	if err != nil {
		return nil, err
	}

	var availablePods []*Pod
	for _, pod := range pods {
		if pod.Status.PodIP != "" {
			availablePods = append(availablePods, pod)
		}
	}

	return availablePods, nil
}

func (k *Client) AssignPod(ctx context.Context, pod *Pod, dest destination.Destination) error {
	patchBody := fmt.Sprintf(`[{"op":"add","path":"/metadata/labels/%s","value":"%s"},{"op":"replace","path":"/metadata/labels/%s","value":"pending"}]`, patchLabelTenant, dest.Tenant, patchLabelStatus)
	_, err := k.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{FieldManager: "fusion/serve"})
	if err != nil {
		return fmt.Errorf("failed to assign pod: %w", err)
	}

	assignCtx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

	req, err := http.NewRequestWithContext(assignCtx, http.MethodPost, "http://"+pod.Status.PodIP+":8080/__fusion/assign", nil)
	if err != nil {
		return fmt.Errorf("failed to create assign request: %w", err)
	}

	req.Header.Set(destination.HeaderTenant, dest.Tenant)
	req.Header.Set(destination.HeaderNamespace, dest.Namespace)
	req.Header.Set(destination.HeaderDeployment, dest.Deployment)
	req.Header.Set(destination.HeaderAssignment, dest.Assignment)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send assign request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("assign request returned status %d", resp.StatusCode)
	}

	patchBody = fmt.Sprintf(`[{"op":"replace","path":"/metadata/labels/%s","value":"ready"}]`, patchLabelStatus)
	_, err = k.clientset.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, []byte(patchBody), metav1.PatchOptions{FieldManager: "fusion/serve"})
	if err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}

	return nil
}

func (k *Client) listPods(namespace string, selector labels.Selector) ([]*Pod, error) {
	listerAny, found := k.listers.Load(namespace)
	if !found {
		k.StartPodListeners([]string{namespace})
		listerAny, _ = k.listers.Load(namespace)
	}

	lister := listerAny.(listerv1.PodLister)
	k8sPods, err := lister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	pods := make([]*Pod, len(k8sPods))
	for i, k8sPod := range k8sPods {
		pods[i] = NewPod(k8sPod)
	}
	return pods, nil
}
