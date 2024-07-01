package k8s

import (
	lctx "context"

	lcoreV1 "k8s.io/api/core/v1"
	lmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	lk8s "k8s.io/client-go/kubernetes"
	lk8srecord "k8s.io/client-go/tools/record"

	lsentity "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/entity"
	lserr "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/errors"
)

type kubernetes struct {
	lk8s.Interface
	lk8srecord.EventRecorder
}

func NewKubernetes(pk8sclient lk8s.Interface, precorder lk8srecord.EventRecorder) IKubernetes {
	return &kubernetes{
		Interface:     pk8sclient,
		EventRecorder: precorder,
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

func (s *kubernetes) PersistentVolumeClaimEventWarning(pctx lctx.Context, pnamespace, pname, preason, pmessage string) {
	if pnamespace == "" || pname == "" {
		return
	}

	pvc, err := s.GetPersistentVolumeClaimByName(pctx, pnamespace, pname)
	if err != nil || pvc == nil {
		return
	}
	s.EventRecorder.Event(pvc.PersistentVolumeClaim, lcoreV1.EventTypeWarning, preason, pmessage)
}

func (s *kubernetes) PersistentVolumeClaimEventNormal(pctx lctx.Context, pnamespace, pname, preason, pmessage string) {
	if pnamespace == "" || pname == "" {
		return
	}

	pvc, err := s.GetPersistentVolumeClaimByName(pctx, pnamespace, pname)
	if err != nil || pvc == nil {
		return
	}
	s.EventRecorder.Event(pvc.PersistentVolumeClaim, lcoreV1.EventTypeNormal, preason, pmessage)
}
