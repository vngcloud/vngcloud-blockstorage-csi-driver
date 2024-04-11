package driver

import (
	"fmt"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/mounter"
	mountutils "k8s.io/mount-utils"
	"os"
	"path/filepath"
	"strings"
)

type Mounter interface {
	mountutils.Interface

	PathExists(path string) (bool, error)
	MakeDir(path string) error
	MakeFile(path string) error
	GetDeviceNameFromMount(mountPath string) (string, int, error)
	FormatAndMountSensitiveWithFormatOptions(source string, target string, fstype string, options []string, sensitiveOptions []string, formatOptions []string) error
	NeedResize(devicePath string, deviceMountPath string) (bool, error)
	NewResizeFs() (Resizefs, error)
	IsCorruptedMnt(err error) bool
	Unpublish(path string) error
}

type DeviceIdentifier interface {
	Lstat(name string) (os.FileInfo, error)
	EvalSymlinks(path string) (string, error)
}

type Resizefs interface {
	Resize(devicePath, deviceMountPath string) (bool, error)
}

// NodeMounter implements Mounter.
// A superstruct of SafeFormatAndMount.
type NodeMounter struct {
	*mountutils.SafeFormatAndMount
}

type nodeDeviceIdentifier struct{}

func newNodeMounter() (Mounter, error) {
	// mounter.NewSafeMounter returns a SafeFormatAndMount
	safeMounter, err := mounter.NewSafeMounter()
	if err != nil {
		return nil, err
	}
	return &NodeMounter{safeMounter}, nil
}

func newNodeDeviceIdentifier() DeviceIdentifier {
	return &nodeDeviceIdentifier{}
}

func (s *NodeMounter) PathExists(path string) (bool, error) {
	return mountutils.PathExists(path)
}

// This function is mirrored in ./sanity_test.go to make sure sanity test covered this block of code
// Please mirror the change to func MakeFile in ./sanity_test.go
func (s *NodeMounter) MakeDir(path string) error {
	err := os.MkdirAll(path, os.FileMode(0755))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// GetDeviceNameFromMount returns the volume ID for a mount path.
func (s NodeMounter) GetDeviceNameFromMount(mountPath string) (string, int, error) {
	return mountutils.GetDeviceNameFromMount(s, mountPath)
}

func (s *NodeMounter) NeedResize(devicePath string, deviceMountPath string) (bool, error) {
	return mountutils.NewResizeFs(s.Exec).NeedResize(devicePath, deviceMountPath)
}

func (s *NodeMounter) NewResizeFs() (Resizefs, error) {
	return mountutils.NewResizeFs(s.Exec), nil
}

// IsCorruptedMnt return true if err is about corrupted mount point
func (s NodeMounter) IsCorruptedMnt(err error) bool {
	return mountutils.IsCorruptedMnt(err)
}

func (s *NodeMounter) Unpublish(path string) error {
	// On linux, unpublish and unstage both perform an unmount
	return s.Unstage(path)
}

func (s *NodeMounter) Unstage(path string) error {
	err := mountutils.CleanupMountPoint(path, s, false)
	// Ignore the error when it contains "not mounted", because that indicates the
	// world is already in the desired state
	//
	// mount-utils attempts to detect this on its own but fails when running on
	// a read-only root filesystem, which our manifests use by default
	if err == nil || strings.Contains(fmt.Sprint(err), "not mounted") {
		return nil
	} else {
		return err
	}
}

func (s *NodeMounter) MakeFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE, os.FileMode(0644))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	if err = f.Close(); err != nil {
		return err
	}
	return nil
}

func (s *nodeDeviceIdentifier) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

func (s *nodeDeviceIdentifier) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
