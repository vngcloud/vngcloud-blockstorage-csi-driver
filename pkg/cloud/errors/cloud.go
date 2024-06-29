package errors

import (
	lfmt "fmt"
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"
)

var (
	ErrVolumeIsInErrorState = func(pvolId string) IError {
		sdkErr := new(lsdkErr.SdkError).
			WithErrorCode(EcVServerVolumeIsInErrorState).
			WithErrors(lfmt.Errorf("volume %s is in error state", pvolId)).
			WithMessage(lfmt.Sprintf("volume %s is in error state", pvolId)).
			WithKVparameters("volumeId", pvolId)

		return NewError(sdkErr)
	}
)
