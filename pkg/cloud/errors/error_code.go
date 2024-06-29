package errors

import lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"

const (
	// Kubernetes errors
	EcK8sFailedToGetPvc = lsdkErrs.ErrorCode("ErrK8sFailedToGetPvc")
)
