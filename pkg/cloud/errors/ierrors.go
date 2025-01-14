package errors

import lsdkErrs "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"

type IError interface {
	lsdkErrs.IError
}
