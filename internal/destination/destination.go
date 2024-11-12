package destination

import (
	"errors"
	"net/http"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

type Destination struct {
	EnvironmentID       string
	DeploymentNamespace string
	DeploymentName      string
	AssignmentSecrets   string
}

func New(req *http.Request) (Destination, error) {
	envID := req.Header.Get("X-Fusion-Environment-Id")
	deploymentName := req.Header.Get("X-Fusion-Deployment-Name")
	deploymentNamespace := req.Header.Get("X-Fusion-Deployment-Namespace")
	assignmentPayload := req.Header.Get("X-Fusion-Assignment-Blob")

	if envID == "" {
		return Destination{}, errors.New("missing required header: X-Fusion-Environment-Id")
	}
	if deploymentName == "" {
		return Destination{}, errors.New("missing required header: X-Fusion-Deployment-Name")
	}
	if deploymentNamespace == "" {
		return Destination{}, errors.New("missing required header: X-Fusion-Deployment-Namespace")
	}
	if assignmentPayload == "" {
		return Destination{}, errors.New("missing required header: X-Fusion-Assignment-Payload")
	}

	return Destination{
		EnvironmentID:       envID,
		DeploymentName:      deploymentName,
		DeploymentNamespace: deploymentNamespace,
		AssignmentSecrets:   assignmentPayload,
	}, nil
}

func (d *Destination) String() string {
	return d.DeploymentNamespace + "/" + d.DeploymentName + "/" + d.EnvironmentID
}

func (d *Destination) AssignedPodsSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{
		"fusion/environment-id":  d.EnvironmentID,
		"fusion/deployment-name": d.DeploymentName,
		"fusion/status":          "ready",
	})
}

func (d *Destination) AvailablePodsSelector() labels.Selector {
	noEnvironmentID, err := labels.NewRequirement("fusion/environment-id", selection.DoesNotExist, nil)
	if err != nil {
		panic(err)
	}

	equalDeploymentName, err := labels.NewRequirement("fusion/deployment-name", selection.Equals, []string{d.DeploymentName})
	if err != nil {
		panic(err)
	}

	return labels.NewSelector().Add(*noEnvironmentID, *equalDeploymentName)
}
