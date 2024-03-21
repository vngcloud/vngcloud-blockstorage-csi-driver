package vcontainer

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

var (
	fakeFileName         = "vcontainer-config.conf"
	fakeOverrideFileName = "cloud-override.conf"
	fakeIdentityURL      = "https://fake-identity-url.vn"
	fakeComputeURL       = "https://fake-compute-url.vn"
	fakeBlockstorageURL  = "https://fake-blockstorage-url.vn"
	fakeClientID         = "fake-client-id"
	fakeClientSecret     = "fake-client-secret"
	fakeCAfile           = "/etc/config/fake-ca.crt"
)

func TestGetConfigFromFiles(t *testing.T) {
	// init file
	var fakeFileContent = `
[Global]
identity-url=` + fakeIdentityURL + `
compute-url=` + fakeComputeURL + `
blockstorage-url=` + fakeBlockstorageURL + `
client-id=` + fakeClientID + `
client-secret=` + fakeClientSecret + `
ca-file=` + fakeCAfile + `
[BlockStorage]
rescan-on-resize=true`

	f, err := os.Create(fakeFileName)
	if err != nil {
		t.Errorf("failed to create file: %v", err)
	}

	_, err = f.WriteString(fakeFileContent)
	f.Close()
	if err != nil {
		t.Errorf("failed to write file: %v", err)
	}
	defer os.Remove(fakeFileName)

	// Init assert
	assert := assert.New(t)
	expectedOpts := Config{}
	expectedOpts.Global.IdentityURL = fakeIdentityURL
	expectedOpts.Global.VServerURL = fakeComputeURL
	expectedOpts.Global.ClientID = fakeClientID
	expectedOpts.Global.ClientSecret = fakeClientSecret
	expectedOpts.BlockStorage.RescanOnResize = true

	// Invoke GetConfigFromFiles
	actualAuthOpts, err := getConfigFromFiles([]string{fakeFileName})
	if err != nil {
		t.Errorf("failed to GetConfigFromFiles: %v", err)
	}

	// Assert
	assert.Equal(expectedOpts, actualAuthOpts)

	// Create an override config file
	var fakeOverrideFileContent = `
[BlockStorage]
rescan-on-resize=false`

	f, err = os.Create(fakeOverrideFileName)
	if err != nil {
		t.Errorf("failed to create file: %v", err)
	}

	_, err = f.WriteString(fakeOverrideFileContent)
	f.Close()
	if err != nil {
		t.Errorf("failed to write file: %v", err)
	}
	defer os.Remove(fakeOverrideFileName)

	// expectedOpts should reflect the overridden value of rescan-on-resize. All
	// other values should be the same as before because they come from the
	// 'base' configuration
	expectedOpts.BlockStorage.RescanOnResize = false

	// Invoke GetConfigFromFiles with both the base and override config files
	actualAuthOpts, err = getConfigFromFiles([]string{fakeFileName, fakeOverrideFileName})
	if err != nil {
		t.Errorf("failed to GetConfigFromFiles: %v", err)
	}

	// Assert
	assert.Equal(expectedOpts, actualAuthOpts)
}
