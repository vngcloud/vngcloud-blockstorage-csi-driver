package mount

import (
	"fmt"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
	"k8s.io/utils/exec"
	"os"
	"path"
	"strings"
)

func GetMountProvider() IMount {
	mountInsOnce.Do(func() {
		mountIns = &Mount{
			BaseMounter: &mount.SafeFormatAndMount{
				Interface: mount.New(""),
				Exec:      exec.New(),
			},
		}
	})

	return mountIns
}

func getDevicePathBySerialID(volumeID string) string {
	// Build a list of candidate device paths.
	// Certain Nova drivers will set the disk serial ID, including the Cinder volume id.
	candidateDeviceNodes := []string{
		// KVM
		fmt.Sprintf("virtio-%s", volumeID[:20]),
		// KVM #852
		fmt.Sprintf("virtio-%s", volumeID),
		// KVM virtio-scsi
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", volumeID[:20]),
		// KVM virtio-scsi #852
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", volumeID),
		// ESXi
		fmt.Sprintf("wwn-0x%s", strings.Replace(volumeID, "-", "", -1)),
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

	klog.V(4).Infof("Failed to find device for the volumeID: %q by serial ID", volumeID)
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
