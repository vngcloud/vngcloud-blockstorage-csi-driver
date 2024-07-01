package errors

import lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"

// PersistentVolumeClaim error group
const (
	EcK8sPvcFailedToGet = lsdkErrs.ErrorCode("K8sPvcFailedToGet")
	EcK8sPvcNotFound    = lsdkErrs.ErrorCode("K8sPvcNotFound")

	EcK8sPvFailedToGet = lsdkErrs.ErrorCode("K8sPvFailedToGet")
	EcK8sPvNotFound    = lsdkErrs.ErrorCode("K8sPvNotFound")
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
	EcVServerVolumeNotFound       = lsdkErrs.ErrorCode("VServerVolumeNotFound")
	EcVServerVolumeFailedToAttach = lsdkErrs.ErrorCode("VServerVolumeFailedToAttach")
)
