package mount

import (
	"fmt"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/blockdevice"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
	"os"
)

type Mount struct {
	BaseMounter *mount.SafeFormatAndMount
}

func (s *Mount) Mounter() *mount.SafeFormatAndMount {
	return s.BaseMounter
}

func (s *Mount) GetDeviceStats(path string) (*DeviceStats, error) {
	isBlock, err := blockdevice.IsBlockDevice(path)
	if err != nil {
		return nil, err
	}

	if isBlock {
		size, err := blockdevice.GetBlockDeviceSize(path)
		if err != nil {
			return nil, err
		}

		return &DeviceStats{
			Block:      true,
			TotalBytes: size,
		}, nil
	}

	var statfs unix.Statfs_t
	// See http://man7.org/linux/man-pages/man2/statfs.2.html for details.
	err = unix.Statfs(path, &statfs)
	if err != nil {
		return nil, err
	}

	return &DeviceStats{
		Block: false,

		AvailableBytes: int64(statfs.Bavail) * int64(statfs.Bsize),
		TotalBytes:     int64(statfs.Blocks) * int64(statfs.Bsize),
		UsedBytes:      (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize),

		AvailableInodes: int64(statfs.Ffree),
		TotalInodes:     int64(statfs.Files),
		UsedInodes:      int64(statfs.Files) - int64(statfs.Ffree),
	}, nil
}

func (s *Mount) GetDevicePath(volumeID string) (string, error) {
	backoff := wait.Backoff{
		Duration: operationFinishInitDelay,
		Factor:   operationFinishFactor,
		Steps:    operationFinishSteps,
	}

	var devicePath string
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		devicePath = getDevicePathBySerialID(volumeID)
		if devicePath != "" {
			return true, nil
		}
		// see issue https://github.com/kubernetes/cloud-provider-openstack/issues/705
		if err := probeVolume(); err != nil {
			// log the error, but continue. Might not happen in edge cases
			klog.V(5).Infof("Unable to probe attached disk: %v", err)
		}
		return false, nil
	})

	if wait.Interrupted(err) {
		return "", fmt.Errorf("failed to find device for the volumeID: %q within the alloted time", volumeID)
	} else if devicePath == "" {
		return "", fmt.Errorf("device path was empty for volumeID: %q", volumeID)
	}
	return devicePath, nil
}

func (s *Mount) IsLikelyNotMountPointAttach(targetPath string) (bool, error) {
	notMnt, err := s.BaseMounter.IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(targetPath, 0750)
			if err == nil {
				notMnt = true
			}
		}
	}
	return notMnt, err
}

func (s *Mount) MakeDir(pathname string) error {
	err := os.MkdirAll(pathname, os.FileMode(0755))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func (s *Mount) MakeFile(pathname string) error {
	f, err := os.OpenFile(pathname, os.O_CREATE, os.FileMode(0644))
	defer func() { _ = f.Close() }()
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func (s *Mount) UnmountPath(mountPath string) error {
	return mount.CleanupMountPoint(mountPath, s.BaseMounter, false /* extensiveMountPointCheck */)
}

func (s *Mount) GetMountFs(volumePath string) ([]byte, error) {
	args := []string{"-o", "source", "--first-only", "--noheadings", "--target", volumePath}
	return s.BaseMounter.Exec.Command("findmnt", args...).CombinedOutput()
}
