package driver

import (
	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	lmath "github.com/cuongpiger/joat/math"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

func getVolSizeBytes(preq *lcsi.CreateVolumeRequest) (volSizeBytes int64, err error) {
	capRange := preq.GetCapacityRange()
	if capRange == nil {
		volSizeBytes = cloud.DefaultVolumeSize
	} else {
		volSizeBytes = util.RoundUpBytes(capRange.GetRequiredBytes())
		maxVolSize := capRange.GetLimitBytes()
		if maxVolSize > 0 && maxVolSize < volSizeBytes {
			return 0, ErrVolumeSizeExceedLimit
		}
	}

	return lmath.MaxNumeric(cloud.DefaultVolumeSize, volSizeBytes), nil
}
