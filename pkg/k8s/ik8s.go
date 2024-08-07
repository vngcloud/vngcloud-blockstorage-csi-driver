package k8s

import (
	lctx "context"

	lsentity "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/entity"
	lserr "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/errors"
)

type IKubernetes interface {
	GetPersistentVolumeClaimByName(pctx lctx.Context, pnamespace, pname string) (*lsentity.PersistentVolumeClaim, lserr.IError)
	GetStorageClassByName(pctx lctx.Context, pname string) (*lsentity.StorageClass, lserr.IError)
	GetPersistentVolume(pctx lctx.Context, pname string) (*lsentity.PersistentVolume, lserr.IError)

	// Event recorder
	PersistentVolumeClaimEventWarning(pctx lctx.Context, pnamespace, pname, preason, pmessage string)
	PersistentVolumeClaimEventNormal(pctx lctx.Context, pnamespace, pname, preason, pmessage string)
	PersistentVolumeEventWarning(pctx lctx.Context, pname, preason, pmessage string)
	PersistentVolumeEventNormal(pctx lctx.Context, pname, preason, pmessage string)
}
