package blockdevice

import (
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"k8s.io/klog/v2"
	"os"
	"path/filepath"
	"strings"
)

func IsBlockDevice(path string) (bool, error) {
	var stat unix.Stat_t
	err := unix.Stat(path, &stat)
	if err != nil {
		return false, fmt.Errorf("failed to stat() %q: %s", path, err)
	}

	return (stat.Mode & unix.S_IFMT) == unix.S_IFBLK, nil
}

func GetBlockDeviceSize(path string) (int64, error) {
	fd, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer fd.Close()
	pos, err := fd.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("error seeking to end of %s: %s", path, err)
	}
	return pos, nil
}

func RescanBlockDeviceGeometry(devicePath string, deviceMountPath string, newSize int64) error {
	if newSize == 0 {
		klog.Error("newSize is empty, skipping the block device rescan")
		return nil
	}

	// when block device size corresponds expectations, return nil
	bdSizeErr := checkBlockDeviceSize(devicePath, deviceMountPath, newSize)
	if bdSizeErr == nil {
		return nil
	}

	// don't fail if resolving doesn't work
	blockDeviceRescanPath, err := findBlockDeviceRescanPath(devicePath)
	if err != nil {
		klog.Errorf("Error resolving block device path from %q: %v", devicePath, err)
		// no need to run checkBlockDeviceSize second time here, return the saved error
		return bdSizeErr
	}

	klog.V(3).Infof("Resolved block device path from %q to %q", devicePath, blockDeviceRescanPath)
	klog.V(4).Infof("Rescanning %q block device geometry", devicePath)
	err = os.WriteFile(blockDeviceRescanPath, []byte{'1'}, 0666)
	if err != nil {
		klog.Errorf("Error rescanning new block device geometry: %v", err)
		// no need to run checkBlockDeviceSize second time here, return the saved error
		return bdSizeErr
	}

	return checkBlockDeviceSize(devicePath, deviceMountPath, newSize)
}

func checkBlockDeviceSize(devicePath string, deviceMountPath string, newSize int64) error {
	klog.V(4).Infof("Detecting %q volume size", deviceMountPath)
	size, err := GetBlockDeviceSize(devicePath)
	if err != nil {
		return err
	}

	klog.V(3).Infof("Detected %q volume size: %d", deviceMountPath, size)

	if size < newSize {
		return fmt.Errorf("current volume size is less than expected one: %d < %d", size, newSize)
	}

	return nil
}

func findBlockDeviceRescanPath(path string) (string, error) {
	devicePath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	// if path /dev/hdX split into "", "dev", "hdX" then we will
	// return just the last part
	parts := strings.Split(devicePath, "/")
	if len(parts) == 3 && strings.HasPrefix(parts[1], "dev") {
		return filepath.EvalSymlinks(filepath.Join("/sys/block", parts[2], "device", "rescan"))
	}
	return "", fmt.Errorf("illegal path for device " + devicePath)
}
