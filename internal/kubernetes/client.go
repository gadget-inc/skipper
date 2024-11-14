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
	"github.com/gadget-inc/fusion/internal/hashring"
	"github.com/gadget-inc/fusion/internal/timer"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
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
	Ring      *hashring.HashRing
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
		Ring:      hashring.New(),
	}, nil
}

func (k *Client) Close() {
	k.cancel()
}

func (k *Client) StartPodListeners(fusionNamespace string, namespaces []string) {
	k.startRouterInformer(fusionNamespace)
	k.startDeploymentInformers(namespaces)
}

func (k *Client) startRouterInformer(fusionNamespace string) error {
	slog.InfoContext(k.ctx, "starting router informer", slog.String("namespace", fusionNamespace))

	factory := informers.NewSharedInformerFactoryWithOptions(
		k.clientset,
		5*time.Minute,
		informers.WithNamespace(fusionNamespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = labels.SelectorFromSet(labels.Set{
				"app.kubernetes.io/name":      "fusion",
				"app.kubernetes.io/component": "router",
			}).String()
		}),
	)

	podInformer := factory.Core().V1().Pods().Informer()

	handler, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				k.Ring.AddNode(hashring.Node{IP: pod.Status.PodIP})
				slog.InfoContext(k.ctx, "added router to ring", slog.String("name", pod.Name), slog.String("namespace", pod.Namespace), slog.String("ip", pod.Status.PodIP))
			}
		},
		UpdateFunc: func(_, newObj any) {
			pod := newObj.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning && pod.Status.PodIP != "" {
				k.Ring.AddNode(hashring.Node{IP: pod.Status.PodIP})
				slog.InfoContext(k.ctx, "updated router in ring", slog.String("name", pod.Name), slog.String("namespace", pod.Namespace), slog.String("ip", pod.Status.PodIP))
			} else {
				k.Ring.RemoveNode(hashring.Node{IP: pod.Status.PodIP})
				slog.InfoContext(k.ctx, "removed updated router from ring", slog.String("name", pod.Name), slog.String("namespace", pod.Namespace), slog.String("ip", pod.Status.PodIP))
			}
		},
		DeleteFunc: func(obj any) {
			pod := obj.(*v1.Pod)
			k.Ring.RemoveNode(hashring.Node{IP: pod.Status.PodIP})
			slog.InfoContext(k.ctx, "removed deleted router from ring", slog.String("name", pod.Name), slog.String("namespace", pod.Namespace), slog.String("ip", pod.Status.PodIP))
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	go podInformer.Run(k.ctx.Done())

	if ok := cache.WaitForCacheSync(k.ctx.Done(), handler.HasSynced); !ok {
		return errors.New("failed to sync router informer cache")
	}

	go func() {
		timer.Loop(k.ctx, 1*time.Second, func(ctx context.Context) error {
			nodes := k.Ring.ListNodes()
			slog.InfoContext(ctx, "ring nodes", slog.Any("nodes", nodes))
			return nil
		})
	}()

	return nil
}

func (k *Client) startDeploymentInformers(namespaces []string) {
	slog.InfoContext(k.ctx, "starting deployment informers", slog.Any("namespaces", namespaces))

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
				slog.ErrorContext(k.ctx, "failed to sync deployment informer cache", slog.String("namespace", namespace))
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
		return nil, fmt.Errorf("pod listeners not started for namespace %s", namespace)
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
