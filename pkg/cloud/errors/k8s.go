package errors

import (
	lfmt "fmt"
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"
)

var (
	ErrK8sFailedToGetPvc = func(pnamespace, pname string, perr error) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcK8sFailedToGetPvc).
			WithMessage(lfmt.Sprintf("Failed to get PVC %s in namespace %s", pname, pnamespace))).
			WithErrors(perr).
			WithParameters(map[string]interface{}{
				"namespace": pnamespace,
				"pvcName":   pname,
			})
	}
)
