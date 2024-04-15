package driver

import (
	"fmt"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/mounter"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	mountutils "k8s.io/mount-utils"
	"k8s.io/utils/exec"
	"os"
	"path"
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
	GetDevicePathBySerialID(pvolId string) (string, error)
	Unstage(path string) error
	GetMountFs(volumePath string) ([]byte, error)
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

func (s *NodeMounter) GetMountFs(volumePath string) ([]byte, error) {
	args := []string{"-o", "source", "--first-only", "--noheadings", "--target", volumePath}
	return s.SafeFormatAndMount.Exec.Command("findmnt", args...).CombinedOutput()
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

func (s *NodeMounter) GetDevicePathBySerialID(pvolId string) (string, error) {
	backoff := wait.Backoff{
		Duration: operationFinishInitDelay,
		Factor:   operationFinishFactor,
		Steps:    operationFinishSteps,
	}

	var devicePath string
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		devicePath = getDevicePathBySerialID(pvolId)
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
		return "", fmt.Errorf("failed to find device for the volumeID: %q within the alloted time", pvolId)
	} else if devicePath == "" {
		return "", fmt.Errorf("device path was empty for volumeID: %q", pvolId)
	}
	return devicePath, nil
}

func getDevicePathBySerialID(pvolID string) string {
	// Build a list of candidate device paths.
	// Certain Nova drivers will set the disk serial ID, including the Cinder volume id.
	candidateDeviceNodes := []string{
		// KVM
		fmt.Sprintf("virtio-%s", pvolID[:20]),
		// KVM #852
		fmt.Sprintf("virtio-%s", pvolID),
		// KVM virtio-scsi
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", pvolID[:20]),
		// KVM virtio-scsi #852
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", pvolID),
		// ESXi
		fmt.Sprintf("wwn-0x%s", strings.Replace(pvolID, "-", "", -1)),
	}

	files, err := os.ReadDir("/dev/disk/by-id/")
	if err != nil {
		klog.V(4).Infof("ReadDir failed with error %v", err)
	}

	for _, f := range files {
		for _, c := range candidateDeviceNodes {
			if c == f.Name() {
				klog.V(4).Infof("Found disk attached as %q; full devicepath: %s\n",
					f.Name(), path.Join("/dev/disk/by-id/", f.Name()))
				return path.Join("/dev/disk/by-id/", f.Name())
			}
		}
	}

	klog.V(4).Infof("Failed to find device for the volumeID: %q by serial ID", pvolID)
	return ""
}

func probeVolume() error {
	// rescan scsi bus
	scsiPath := "/sys/class/scsi_host/"
	if dirs, err := os.ReadDir(scsiPath); err == nil {
		for _, f := range dirs {
			name := scsiPath + f.Name() + "/scan"
			data := []byte("- - -")
			if err := os.WriteFile(name, data, 0666); err != nil {
				return fmt.Errorf("Unable to scan %s: %w", f.Name(), err)
			}
		}
	}

	executor := exec.New()
	args := []string{"trigger"}
	cmd := executor.Command("udevadm", args...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		klog.V(3).Infof("error running udevadm trigger %v\n", err)
		return err
	}
	return nil
}
