package main

import (
	"context"
	"fmt"
	flag "github.com/spf13/pflag"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/metrics"
	"k8s.io/component-base/featuregate"
	"os"
	"strings"

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
		driver.WithClusterID(options.ServerOptions.ClusterID),
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

var (
	featureGate = featuregate.NewFeatureGate()

	// used for testing
	osExit = os.Exit
)

type Options struct {
	DriverMode driver.Mode

	*ServerOptions
	*ControllerOptions
	*NodeOptions
	*Global
}

type Global struct {
	IdentityURL  string `gcfg:"identity-url" mapstructure:"identity-url" name:"identity-url"`
	VServerURL   string `gcfg:"vserver-url" mapstructure:"vserver-url" name:"vserver-url"`
	ClientID     string `gcfg:"client-id" mapstructure:"client-id" name:"client-id"`
	ClientSecret string `gcfg:"client-secret" mapstructure:"client-secret" name:"client-secret"`
}

type ServerOptions struct {
	Endpoint          string
	HttpEndpoint      string
	ClusterID         string
	EnableOtelTracing bool
}

func (s *ServerOptions) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.Endpoint, "endpoint", driver.DefaultCSIEndpoint, "Endpoint for the CSI driver server")
	fs.StringVar(&s.HttpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for metrics will listen (example: `:8080`). The default is empty string, which means the server is disabled.")
	fs.BoolVar(&s.EnableOtelTracing, "enable-otel-tracing", false, "To enable opentelemetry tracing for the driver. The tracing is disabled by default. Configure the exporter endpoint with OTEL_EXPORTER_OTLP_ENDPOINT and other env variables, see https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/#general-sdk-configuration.")
	fs.StringVar(&s.ClusterID, "cluster-id", "", "The unique ID of the cluster. This is used to identify the cluster in the logs.")
}

type ControllerOptions struct {
	ModifyVolumeRequestHandlerTimeout time.Duration
	UserAgentExtra                    string
}

func (s *ControllerOptions) AddFlags(fs *flag.FlagSet) {
	fs.DurationVar(&s.ModifyVolumeRequestHandlerTimeout, "modify-volume-request-handler-timeout", driver.DefaultModifyVolumeRequestHandlerTimeout, "Timeout for the window in which volume modification calls must be received in order for them to coalesce into a single volume modification call to AWS. This must be lower than the csi-resizer and volumemodifier timeouts")
	fs.StringVar(&s.UserAgentExtra, "user-agent-extra", "", "Extra string appended to user agent.")

}

type NodeOptions struct{}

func (s *NodeOptions) AddFlags(fs *flag.FlagSet) {
}

func (s *NodeOptions) Validate() error {
	return nil
}

func GetOptions(fs *flag.FlagSet) *Options {
	var (
		version  = fs.Bool("version", false, "Print the version and exit.")
		toStderr = fs.Bool("logtostderr", false, "log to standard error instead of files. DEPRECATED: will be removed in a future release.")

		args = os.Args[1:]
		cmd  = string(driver.AllMode)

		serverOptions     = ServerOptions{}
		controllerOptions = ControllerOptions{}
		nodeOptions       = NodeOptions{}
	)

	serverOptions.AddFlags(fs)
	c := logsapi.NewLoggingConfiguration()

	err := logsapi.AddFeatureGates(featureGate)
	if err != nil {
		klog.ErrorS(err, "failed to add feature gates")
	}

	logsapi.AddFlags(c, fs)

	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
		args = os.Args[2:]
	}

	switch cmd {
	case string(driver.ControllerMode):
		controllerOptions.AddFlags(fs)

	case string(driver.NodeMode):
		nodeOptions.AddFlags(fs)

	case string(driver.AllMode):
		controllerOptions.AddFlags(fs)
		nodeOptions.AddFlags(fs)

	default:
		klog.Errorf("Unknown driver mode %s: Expected %s, %s, %s", cmd, driver.ControllerMode, driver.NodeMode, driver.AllMode)
		klog.FlushAndExit(klog.ExitFlushTimeout, 0)
	}

	if err = fs.Parse(args); err != nil {
		panic(err)
	}

	if cmd != string(driver.ControllerMode) {
		// nodeOptions must have been populated from the cmdline, validate them.
		if err := nodeOptions.Validate(); err != nil {
			klog.Error(err.Error())
			klog.FlushAndExit(klog.ExitFlushTimeout, 1)
		}
	}

	err = logsapi.ValidateAndApply(c, featureGate)
	if err != nil {
		klog.ErrorS(err, "failed to validate and apply logging configuration")
	}

	if *version {
		versionInfo, err := driver.GetVersionJSON()
		if err != nil {
			klog.ErrorS(err, "failed to get version")
			klog.FlushAndExit(klog.ExitFlushTimeout, 1)
		}
		fmt.Println(versionInfo)
		osExit(0)
	}

	if *toStderr {
		klog.SetOutput(os.Stderr)
	}

	config, err := getConfigFromEnv()
	if err != nil {
		klog.Errorf("Failed to get config from files: %v", err)
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	options := &Options{
		DriverMode:        driver.Mode(cmd),
		ServerOptions:     &serverOptions,
		ControllerOptions: &controllerOptions,
		NodeOptions:       &nodeOptions,
		Global:            config,
	}

	return options
}

func getConfigFromEnv() (*Global, error) {
	clientID := os.Getenv("VNGCLOUD_ACCESS_KEY_ID")
	clientSecret := os.Getenv("VNGCLOUD_SECRET_ACCESS_KEY")
	identityEndpoint := os.Getenv("VNGCLOUD_IDENTITY_ENDPOINT")
	vserverEndpoint := os.Getenv("VNGCLOUD_VSERVER_ENDPOINT")

	if clientID == "" || clientSecret == "" || identityEndpoint == "" || vserverEndpoint == "" {
		return nil, fmt.Errorf("missing required environment variables")
	}

	var cfg Global
	cfg.ClientID = clientID
	cfg.ClientSecret = clientSecret
	cfg.IdentityURL = identityEndpoint
	cfg.VServerURL = vserverEndpoint

	return &cfg, nil
}
