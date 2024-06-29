package k8s

import (
	lctx "context"

	lcorev1 "k8s.io/api/core/v1"
	lmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	lk8s "k8s.io/client-go/kubernetes"

	lserr "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/errors"
)

type kubernetes struct {
	lk8s.Interface
}

func NewKubernetes(pk8sclient lk8s.Interface) IKubernetes {
	return &kubernetes{
		Interface: pk8sclient,
	}
}

func (s *kubernetes) GetPersistentVolumeClaimByName(pctx lctx.Context, pnamespace, pname string) (*lcorev1.PersistentVolumeClaim, lserr.IError) {
	pvc, err := s.CoreV1().PersistentVolumeClaims(pnamespace).Get(pctx, pname, lmetav1.GetOptions{})
	if err != nil {
		return nil, lserr.ErrK8sFailedToGetPvc(pnamespace, pname, err)
	}

	return pvc, nil
}
