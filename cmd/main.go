package main

import (
	"context"
	flag "github.com/spf13/pflag"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/metrics"

	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/logs/json"
	"k8s.io/klog/v2"
	"time"
)

func main() {
	fs := flag.NewFlagSet("vngcloud-blockstorage-csi-driver", flag.ExitOnError)
	if err := logsapi.RegisterLogFormat(logsapi.JSONLogFormat, json.Factory{}, logsapi.LoggingBetaOptions); err != nil {
		klog.ErrorS(err, "failed to register JSON log format")
	}

	options := GetOptions(fs)
	// Start tracing as soon as possible
	if options.ServerOptions.EnableOtelTracing {
		exporter, err := driver.InitOtelTracing()
		if err != nil {
			klog.ErrorS(err, "failed to initialize otel tracing")
			klog.FlushAndExit(klog.ExitFlushTimeout, 1)
		}
		// Exporter will flush traces on shutdown
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := exporter.Shutdown(ctx); err != nil {
				klog.ErrorS(err, "could not shutdown otel exporter")
			}
		}()
	}

	if options.ServerOptions.HttpEndpoint != "" {
		r := metrics.InitializeRecorder()
		r.InitializeMetricsHandler(options.ServerOptions.HttpEndpoint, "/metrics")
	}

	drv, err := driver.NewDriver(
		driver.WithClientID(options.Global.ClientID),
		driver.WithClientSecret(options.Global.ClientSecret),
		driver.WithIdentityURL(options.Global.IdentityURL),
		driver.WithVServerURL(options.Global.VServerURL),
		driver.WithEndpoint(options.ServerOptions.Endpoint),
		driver.WithMode(options.DriverMode),
		driver.WithOtelTracing(options.ServerOptions.EnableOtelTracing),
		driver.WithModifyVolumeRequestHandlerTimeout(options.ControllerOptions.ModifyVolumeRequestHandlerTimeout),
	)

	if err != nil {
		klog.ErrorS(err, "failed to create driver")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := drv.Run(); err != nil {
		klog.ErrorS(err, "failed to run driver")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}
