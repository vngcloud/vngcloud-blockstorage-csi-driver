package k8s

import (
	lctx "context"

	lcorev1 "k8s.io/api/core/v1"

	lserr "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/errors"
)

type IKubernetes interface {
	GetPersistentVolumeClaimByName(pctx lctx.Context, pnamespace, pname string) (*lcorev1.PersistentVolumeClaim, lserr.IError)
}
