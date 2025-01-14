package errors

import lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"

type errors struct {
	lsdkErrs.IError
}

func NewError(psdkErr lsdkErrs.IError) IError {
	return &errors{psdkErr}
}
