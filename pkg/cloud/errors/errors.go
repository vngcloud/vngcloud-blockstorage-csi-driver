package errors

import lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"

type errors struct {
	lsdkErrs.ISdkError
}

func NewError(psdkErr lsdkErrs.ISdkError) IError {
	return &errors{psdkErr}
}
