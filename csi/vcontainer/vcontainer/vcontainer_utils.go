package vcontainer

import (
	"fmt"
	"github.com/cuongpiger/joat/utils"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/client"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/metrics"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/metadata"
	vclient "github.com/vngcloud/vngcloud-go-sdk/client"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud"
	gcfg "gopkg.in/gcfg.v1"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
	"net/http"
	"os"
	"strconv"
)

// *************************************************** PUBLIC METHODS **************************************************

func InitProvider(cfgFiles []string, httpEndpoint string) {
	metrics.RegisterMetrics("vcontainer-csi-blockstorage")
	if httpEndpoint != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", legacyregistry.HandlerWithReset())
		go func() {
			err := http.ListenAndServe(httpEndpoint, mux)
			if err != nil {
				klog.Fatalf("InitProvider; failed to listen & serve metrics from %q: %v", httpEndpoint, err)
			}
			klog.Infof("InitProvider; metrics available in %q", httpEndpoint)
		}()
	}

	configFiles = cfgFiles // assign the CLI config files to global variable
	klog.V(2).Infof("InitProvider; configFiles: %s", configFiles)
}

func GetProvider() (IVContainer, error) {
	var err error
	vcontainerInsOnce.Do(func() {
		vcontainerIns, err = createProvider()
	})

	if err != nil {
		return nil, err
	}

	return vcontainerIns, nil
}

func NewVContainer(compute, blockstorage, portal *vclient.ServiceClient, bsOpts BlockStorageOpts, metadataOpts metadata.Opts) IVContainer {
	if metadataOpts.SearchOrder == "" {
		metadataOpts.SearchOrder = fmt.Sprintf("%s,%s", metadata.ConfigDriveID, metadata.MetadataID)
	}

	klog.Infof("NewVContainer; metadataOpts is %+v", metadataOpts)

	return &vContainer{
		compute:      compute,
		blockstorage: blockstorage,
		portal:       portal,
		bsOpts:       bsOpts,
		metadataOpts: metadataOpts,
	}
}

// ************************************************** PRIVATE METHODS **************************************************

func createProvider() (IVContainer, error) {
	klog.Info("createVContainerProvider; configFiles: ", configFiles)

	cfg, err := getConfigFromFiles(configFiles)
	if err != nil {
		klog.Errorf("createProvider; failed to get config from files; ERR: %v", err)
		return nil, err
	}
	logcfg(cfg)

	vserverV1 := utils.NormalizeURL(cfg.Global.VServerURL) + "v1"
	vserverV2 := utils.NormalizeURL(cfg.Global.VServerURL) + "v2"

	provider, err := client.NewVContainerClient(&cfg.Global)
	computeClient, _ := vngcloud.NewServiceClient(vserverV2, provider, "vserver")
	blockstorageClient, _ := vngcloud.NewServiceClient(vserverV2, provider, "blockstorage")
	portalClient, _ := vngcloud.NewServiceClient(vserverV1, provider, "portal")
	vcon := NewVContainer(computeClient, blockstorageClient, portalClient, cfg.BlockStorage, cfg.Metadata)

	return vcon, nil
}

func getConfigFromFiles(configFiles []string) (Config, error) {
	var cfg Config

	// Read all specified config files in order. Values from later config files
	// will overwrite values from earlier ones.
	for _, configFilePath := range configFiles {
		config, err := os.Open(configFilePath)
		if err != nil {
			klog.Errorf("Failed to open OpenStack configuration file: %v", err)
			return cfg, err
		}
		defer config.Close()

		err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
		if err != nil {
			klog.Errorf("Failed to read OpenStack configuration file: %v", err)
			return cfg, err
		}
	}

	return cfg, nil
}

func logcfg(cfg Config) {
	client.LogCfg(cfg.Global)
	klog.Infof("Block storage opts: %v", cfg.BlockStorage)
}

func standardPaging(limit int, startingToken string) (int, int) {
	var page, size int

	if limit < 1 {
		size = defaultPageSize
	} else {
		size = limit
	}

	if p, err := strconv.Atoi(startingToken); err != nil {
		page = defaultFirstPage
	} else {
		page = p
	}

	return page, size
}
