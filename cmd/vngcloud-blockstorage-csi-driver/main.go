package main

import (
	"flag"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/driver"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/metadata"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/mount"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/vcontainer/vcontainer"
	"k8s.io/component-base/cli"
	"k8s.io/klog/v2"
	"os"
)

var (
	endpoint         string
	httpEndpoint     string
	vcontainerConfig []string
)

func main() {
	if err := flag.CommandLine.Parse([]string{}); err != nil {
		klog.Fatalf("main; Unable to parse flags; ERR: %v", err)
	}

	cmd := &cobra.Command{
		Use:   "vngcloud-blockstorage-csi-driver",
		Short: "vContainer CSI plugin for Kubernetes",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Glog requires this otherwise it complains.
			if err := flag.CommandLine.Parse(nil); err != nil {
				return fmt.Errorf("unable to parse flags: %w", err)
			}

			// This is a temporary hack to enable proper logging until upstream dependencies
			// are migrated to fully utilize klog instead of glog.
			klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
			klog.InitFlags(klogFlags)

			// Sync the glog and klog flags.
			cmd.Flags().VisitAll(func(f1 *pflag.Flag) {
				f2 := klogFlags.Lookup(f1.Name)
				if f2 != nil {
					value := f1.Value.String()
					_ = f2.Value.Set(value)
				}
			})
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			handle()
		},
	}

	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "vContainer CSI block-storage endpoint")
	if err := cmd.MarkPersistentFlagRequired("endpoint"); err != nil {
		klog.Fatalf("main; Unable to mark flag endpoint to be required; ERR: %v", err)
	}

	cmd.PersistentFlags().StringSliceVar(&vcontainerConfig, "vcontainer-config", nil, "CSI driver config for vContainer. This option can be given multiple times")
	if err := cmd.MarkPersistentFlagRequired("vcontainer-config"); err != nil {
		klog.Fatalf("main; Unable to mark flag vcontainer-config to be required; ERR: %v", err)
	}

	cmd.PersistentFlags().StringVar(&httpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for diagnostics, including metrics and leader election health check, will listen (example: `:8080`). The default is empty string, which means the server is disabled.")

	code := cli.Run(cmd)
	os.Exit(code)
}

func handle() {
	drv := driver.NewDriver(endpoint)
	vcontainer.InitProvider(vcontainerConfig, httpEndpoint)
	provider, err := vcontainer.GetProvider()
	if err != nil {
		klog.Fatalf("handle; failed to get provider; ERR: %v", err)
		return
	}

	mounter := mount.GetMountProvider()
	metadator := metadata.GetMetadataProvider(provider.GetMetadataOpts().SearchOrder)
	if err := provider.SetupPortalInfo(metadator); err != nil {
		klog.Fatalf("handle; failed to setup portal info; ERR: %v", err)
		return
	}

	drv.SetupDriver(provider, mounter, metadator)
	drv.Run()
}
