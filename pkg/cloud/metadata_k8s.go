package cloud

import (
	lk8score "k8s.io/api/core/v1"
	lk8s "k8s.io/client-go/kubernetes"
	lk8sscheme "k8s.io/client-go/kubernetes/scheme"
	lcoreV1 "k8s.io/client-go/kubernetes/typed/core/v1"
	lk8srest "k8s.io/client-go/rest"
	lk8srecord "k8s.io/client-go/tools/record"
)

// KubernetesAPIClient is a function that returns a Kubernetes client
type KubernetesAPIClient func() (lk8s.Interface, error)

var DefaultKubernetesAPIClient = func() (lk8s.Interface, error) {
	// Creates the in-cluster config
	config, err := lk8srest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	// creates the clientset
	clientset, err := lk8s.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

var DefaultEventRecorder = func(pclient lk8s.Interface) (lk8srecord.EventBroadcaster, lk8srecord.EventRecorder, error) {
	broadcaster := lk8srecord.NewBroadcaster()
	broadcaster.StartRecordingToSink(&lcoreV1.EventSinkImpl{Interface: pclient.CoreV1().Events("")})
	eventRecorder := broadcaster.NewRecorder(lk8sscheme.Scheme, lk8score.EventSource{Component: "vngcloud-blockstorage-csi-driver"})
	return broadcaster, eventRecorder, nil
}
