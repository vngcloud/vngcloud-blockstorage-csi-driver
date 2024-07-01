package errors

import lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"

// PersistentVolumeClaim error group
const (
	EcK8sPvcFailedToGet = lsdkErrs.ErrorCode("K8sPvcFailedToGet")
	EcK8sPvcNotFound    = lsdkErrs.ErrorCode("K8sPvcNotFound")
)

// StorageClass error group
const (
	EcK8sStorageClassFailedToGet = lsdkErrs.ErrorCode("K8sStorageClassFailedToGet")
	EcK8sStorageClassNotFound    = lsdkErrs.ErrorCode("K8sStorageClassNotFound")
)

// VngCloud BlockStorage Volume
const (
	EcVServerVolumeIsInErrorState = lsdkErrs.ErrorCode("VServerVolumeIsInErrorState")
	EcVServerVolumeFailedToDetach = lsdkErrs.ErrorCode("VServerVolumeFailedToDetach")
	EcVServerVolumeFailedToGet    = lsdkErrs.ErrorCode("VServerVolumeFailedToGet")
	EcVServerVolumeFailedToDelete = lsdkErrs.ErrorCode("VServerVolumeFailedToDelete")
)
