package errors

import (
	lfmt "fmt"
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"
)

// PersistentVolumeClaim error group
var (
	ErrK8sPvcFailedToGet = func(pnamespace, pname string, perr error) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcK8sPvcFailedToGet).
			WithMessage(lfmt.Sprintf("Failed to get PVC %s in namespace %s", pname, pnamespace))).
			WithErrors(perr).
			WithParameters(map[string]interface{}{
				"namespace": pnamespace,
				"pvcName":   pname,
			})
	}

	ErrK8sPvcNotFound = func(pnamespace, pname string) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcK8sPvcNotFound).
			WithMessage(lfmt.Sprintf("PVC %s in namespace %s not found", pname, pnamespace))).
			WithParameters(map[string]interface{}{
				"namespace": pnamespace,
				"pvcName":   pname,
			})
	}
)

// StorageClass error group
var (
	ErrK8sStorageClassFailedToGet = func(pname string, perr error) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcK8sStorageClassFailedToGet).
			WithMessage(lfmt.Sprintf("Failed to get StorageClass %s", pname))).
			WithErrors(perr).
			WithParameters(map[string]interface{}{
				"storageClassName": pname,
			})
	}

	ErrK8sStorageClassNotFound = func(pname string) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcK8sStorageClassNotFound).
			WithMessage(lfmt.Sprintf("StorageClass %s not found", pname))).
			WithParameters(map[string]interface{}{
				"storageClassName": pname,
			})
	}
)
