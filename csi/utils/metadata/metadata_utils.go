package metadata

import (
	"encoding/json"
	"fmt"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/mount"
	"io"
	"k8s.io/klog/v2"
	"k8s.io/utils/exec"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func GetMetadataProvider(order string) IMetadata {
	metadataInsOnce.Do(func() {
		metadataIns = &metadataService{
			searchOrder: order,
		}
	})

	return metadataIns
}

func Get(order string) (*Metadata, error) {
	if metadataCache == nil {
		var md *Metadata
		var err error

		elements := strings.Split(order, ",")
		for _, id := range elements {
			id = strings.TrimSpace(id)
			switch id {
			case ConfigDriveID:
				md, err = getFromConfigDrive(defaultMetadataVersion)
			case MetadataID:
				md, err = getFromMetadataService(defaultMetadataVersion)
			default:
				err = fmt.Errorf("%s is not a valid metadata search order option. Supported options are %s and %s", id, ConfigDriveID, MetadataID)
			}

			if err == nil {
				break
			}
		}

		if err != nil {
			return nil, err
		}
		metadataCache = md
	}

	return metadataCache, nil
}

// ************************************************** PRIVATE METHODS **************************************************

func getFromConfigDrive(metadataVersion string) (*Metadata, error) {
	// Try to read instance UUID from config drive.
	dev := "/dev/disk/by-label/" + configDriveLabel
	if _, err := os.Stat(dev); os.IsNotExist(err) {
		out, err := exec.New().Command(
			"blkid", "-l",
			"-t", "LABEL="+configDriveLabel,
			"-o", "device",
		).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("getFromConfigDrive; unable to run blkid; ERR: %v", err)
		}
		dev = strings.TrimSpace(string(out))
	}

	mntdir := os.TempDir()
	defer os.Remove(mntdir)

	klog.V(4).Infof("getFromConfigDrive; attempting to mount configdrive %s on %s", dev, mntdir)

	mounter := mount.GetMountProvider().Mounter()
	err := mounter.Mount(dev, mntdir, "iso9660", []string{"ro"})
	if err != nil {
		err = mounter.Mount(dev, mntdir, "vfat", []string{"ro"})
	}
	if err != nil {
		return nil, fmt.Errorf("getFromConfigDrive; error mounting configdrive %s; ERR: %v", dev, err)
	}
	defer func() { _ = mounter.Unmount(mntdir) }()

	klog.V(4).Infof("getFromConfigDrive; configdrive mounted on %s", mntdir)

	configDrivePath := getConfigDrivePath(metadataVersion)
	f, err := os.Open(
		filepath.Join(mntdir, configDrivePath))
	if err != nil {
		return nil, fmt.Errorf("getFromConfigDrive; error reading %s on config drive; ERR: %v", configDrivePath, err)
	}
	defer f.Close()

	return parseMetadata(f)
}

func getConfigDrivePath(metadataVersion string) string {
	return fmt.Sprintf(configDrivePathTemplate, metadataVersion)
}

func parseMetadata(r io.Reader) (*Metadata, error) {
	var metadata Metadata
	jsonDecoder := json.NewDecoder(r)
	if err := jsonDecoder.Decode(&metadata); err != nil {
		return nil, err
	}

	if metadata.UUID == "" {
		return nil, ErrBadMetadata
	}

	return &metadata, nil
}

func getFromMetadataService(metadataVersion string) (*Metadata, error) {
	// Try to get JSON from metadata server.
	metadataURL := getMetadataURL(metadataVersion)
	klog.V(4).Infof("getFromMetadataService; attempting to fetch metadata from %s, ignoring proxy settings", metadataURL)
	resp, err := noProxyHTTPClient().Get(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("getFromMetadataService; error fetching metadata from URL %s; ERR: %v", metadataURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("getFromMetadataService; unexpected status code when reading metadata from %s with response status code %s", metadataURL, resp.Status)
		return nil, err
	}

	return parseMetadata(resp.Body)
}

func getMetadataURL(metadataVersion string) string {
	return fmt.Sprintf(metadataURLTemplate, metadataVersion)
}

func noProxyHTTPClient() *http.Client {
	noProxyTransport := http.DefaultTransport.(*http.Transport).Clone()
	noProxyTransport.Proxy = nil
	return &http.Client{Transport: noProxyTransport}
}
