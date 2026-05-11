package controller

import (
	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/fixture"
	v1 "k8s.io/api/core/v1"
)

func testConfig() *Config {
	cfg := config.New[Config]()
	cfg.Namespace = fixture.ControllerNamespace
	cfg.PodIP = fixture.ControllerIP
	cfg.PasetoPrivateKey = PasetoPrivateKey{V2AsymmetricSecretKey: fixture.ControllerPasetoSecretKey}
	cfg.AssignmentNamespaces = []string{fixture.AssignmentNamespace}
	return cfg
}

func countReadyAndUnreadyPods(pods []v1.Pod) (ready, unready int) {
	for _, pod := range pods {
		if isPodReady(&pod) {
			ready++
		} else if isPodRunning(&pod) {
			unready++
		}
	}
	return
}
