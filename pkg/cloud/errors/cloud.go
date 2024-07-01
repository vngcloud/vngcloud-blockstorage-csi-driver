package errors

import (
	lfmt "fmt"
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/sdk_error"
)

var (
	ErrVolumeIsInErrorState = func(pvolId string) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcVServerVolumeIsInErrorState).
			WithErrors(lfmt.Errorf("volume %s is in error state", pvolId)).
			WithMessage(lfmt.Sprintf("volume %s is in error state", pvolId)).
			WithKVparameters("volumeId", pvolId))
	}

	ErrVolumeFailedToDetach = func(pinstanceId, pvolId string, psdkErr lsdkErr.ISdkError) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcVServerVolumeFailedToDetach).
			WithErrors(psdkErr.GetError()).
			WithMessage(lfmt.Sprintf("Failed to detach volume %s from instance %s", pvolId, pinstanceId)).
			WithKVparameters("instanceId", pinstanceId, "volumeId", pvolId).
			WithParameters(psdkErr.GetParameters()))
	}

	ErrVServerVolumeFailedToGet = func(pvolId string, psdkErr lsdkErr.ISdkError) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcVServerVolumeFailedToGet).
			WithErrors(psdkErr.GetError()).
			WithMessage(lfmt.Sprintf("Failed to get volume %s", pvolId)).
			WithKVparameters("volumeId", pvolId).
			WithParameters(psdkErr.GetParameters()))
	}

	ErrVServerVolumeFailedToDelete = func(pvolId string, psdkErr lsdkErr.ISdkError) IError {
		return NewError(new(lsdkErr.SdkError).
			WithErrorCode(EcVServerVolumeFailedToDelete).
			WithErrors(psdkErr.GetError()).
			WithMessage(lfmt.Sprintf("Failed to delete volume %s", pvolId)).
			WithKVparameters("volumeId", pvolId).
			WithParameters(psdkErr.GetParameters()))
	}
)
