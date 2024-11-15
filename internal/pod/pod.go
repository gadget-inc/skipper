package pod

import (
	v1 "k8s.io/api/core/v1"
)

type Pod struct {
	*v1.Pod
}

func New(pod *v1.Pod) *Pod {
	return &Pod{
		Pod: pod,
	}
}
