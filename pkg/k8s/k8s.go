package k8s

import (
	lctx "context"

	lmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	lk8s "k8s.io/client-go/kubernetes"

	lsentity "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/entity"
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

func (s *kubernetes) GetPersistentVolumeClaimByName(pctx lctx.Context, pnamespace, pname string) (*lsentity.PersistentVolumeClaim, lserr.IError) {
	pvc, err := s.CoreV1().PersistentVolumeClaims(pnamespace).Get(pctx, pname, lmetav1.GetOptions{})
	if err != nil {
		return nil, lserr.ErrK8sPvcFailedToGet(pnamespace, pname, err)
	}

	if pvc == nil {
		return nil, lserr.ErrK8sPvcNotFound(pnamespace, pname)
	}

	return lsentity.NewPersistentVolumeClaim(pvc), nil
}

func (s *kubernetes) GetStorageClassByName(pctx lctx.Context, pname string) (*lsentity.StorageClass, lserr.IError) {
	sc, err := s.StorageV1().StorageClasses().Get(pctx, pname, lmetav1.GetOptions{})
	if err != nil {
		return nil, lserr.ErrK8sStorageClassFailedToGet(pname, err)
	}

	if sc == nil {
		return nil, lserr.ErrK8sStorageClassNotFound(pname)
	}

	return lsentity.NewStorageClass(sc), nil
}
