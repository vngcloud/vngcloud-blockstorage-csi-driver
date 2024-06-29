package main

import (
	lctx "context"
	lfmt "fmt"
	los "os"
	lstr "strings"
	ltime "time"

	lflag "github.com/spf13/pflag"
	lfeaturegate "k8s.io/component-base/featuregate"
	llogApi "k8s.io/component-base/logs/api/v1"
	ljson "k8s.io/component-base/logs/json"
	llog "k8s.io/klog/v2"

	lsdriver "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver"
	lsmetrics "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/metrics"
)

func main() {
	fs := lflag.NewFlagSet("vngcloud-blockstorage-csi-driver", lflag.ExitOnError)
	if err := llogApi.RegisterLogFormat(llogApi.JSONLogFormat, ljson.Factory{}, llogApi.LoggingBetaOptions); err != nil {
		llog.ErrorS(err, "[ERROR] - main: Failed to register JSON log format")
	}

	options := GetOptions(fs)
	// Start tracing as soon as possible
	if options.ServerOptions.EnableOtelTracing {
		exporter, err := lsdriver.InitOtelTracing()
		if err != nil {
			llog.ErrorS(err, "failed to initialize otel tracing")
			llog.FlushAndExit(llog.ExitFlushTimeout, 1)
		}
		// Exporter will flush traces on shutdown
		defer func() {
			ctx, cancel := lctx.WithTimeout(lctx.Background(), 10*ltime.Second)
			defer cancel()
			if err := exporter.Shutdown(ctx); err != nil {
				llog.ErrorS(err, "could not shutdown otel exporter")
			}
		}()
	}

	if options.ServerOptions.HttpEndpoint != "" {
		r := lsmetrics.InitializeRecorder()
		r.InitializeMetricsHandler(options.ServerOptions.HttpEndpoint, "/metrics")
	}

	drv, err := lsdriver.NewDriver(
		lsdriver.WithClientID(options.Global.ClientID),
		lsdriver.WithClientSecret(options.Global.ClientSecret),
		lsdriver.WithIdentityURL(options.Global.IdentityURL),
		lsdriver.WithVServerURL(options.Global.VServerURL),
		lsdriver.WithEndpoint(options.ServerOptions.Endpoint),
		lsdriver.WithMode(options.DriverMode),
		lsdriver.WithOtelTracing(options.ServerOptions.EnableOtelTracing),
		lsdriver.WithModifyVolumeRequestHandlerTimeout(options.ControllerOptions.ModifyVolumeRequestHandlerTimeout),
		lsdriver.WithClusterID(options.ServerOptions.ClusterID),
		lsdriver.WithTagKeyLength(options.ServerOptions.TagKeyLength),
		lsdriver.WithTagValueLength(options.ServerOptions.TagValueLength),
	)

	if err != nil {
		llog.ErrorS(err, "failed to create driver")
		llog.FlushAndExit(llog.ExitFlushTimeout, 1)
	}
	if err := drv.Run(); err != nil {
		llog.ErrorS(err, "failed to run driver")
		llog.FlushAndExit(llog.ExitFlushTimeout, 1)
	}
}

var (
	featureGate = lfeaturegate.NewFeatureGate()

	// used for testing
	osExit = los.Exit
)

type Options struct {
	DriverMode lsdriver.Mode

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
	TagKeyLength      int
	TagValueLength    int
}

func (s *ServerOptions) AddFlags(fs *lflag.FlagSet) {
	fs.StringVar(&s.Endpoint, "endpoint", lsdriver.DefaultCSIEndpoint, "Endpoint for the CSI driver server")
	fs.StringVar(&s.HttpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for metrics will listen (example: `:8080`). The default is empty string, which means the server is disabled.")
	fs.BoolVar(&s.EnableOtelTracing, "enable-otel-tracing", false, "To enable opentelemetry tracing for the driver. The tracing is disabled by default. Configure the exporter endpoint with OTEL_EXPORTER_OTLP_ENDPOINT and other env variables, see https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/#general-sdk-configuration.")
	fs.StringVar(&s.ClusterID, "cluster-id", "", "The unique ID of the cluster. This is used to identify the cluster in the logs.")
	fs.IntVar(&s.TagKeyLength, "tag-key-length", 15, "The maximum length of the tag key.")
	fs.IntVar(&s.TagValueLength, "tag-value-length", 255, "The maximum length of the tag value.")
}

type ControllerOptions struct {
	ModifyVolumeRequestHandlerTimeout ltime.Duration
	UserAgentExtra                    string
}

func (s *ControllerOptions) AddFlags(fs *lflag.FlagSet) {
	fs.DurationVar(&s.ModifyVolumeRequestHandlerTimeout, "modify-volume-request-handler-timeout", lsdriver.DefaultModifyVolumeRequestHandlerTimeout, "Timeout for the window in which volume modification calls must be received in order for them to coalesce into a single volume modification call to AWS. This must be lower than the csi-resizer and volumemodifier timeouts")
	fs.StringVar(&s.UserAgentExtra, "user-agent-extra", "", "Extra string appended to user agent.")

}

type NodeOptions struct{}

func (s *NodeOptions) AddFlags(fs *lflag.FlagSet) {
}

func (s *NodeOptions) Validate() error {
	return nil
}

func GetOptions(fs *lflag.FlagSet) *Options {
	var (
		version  = fs.Bool("version", false, "Print the version and exit.")
		toStderr = fs.Bool("logtostderr", false, "log to standard error instead of files. DEPRECATED: will be removed in a future release.")

		args = los.Args[1:]
		cmd  = string(lsdriver.AllMode)

		serverOptions     = ServerOptions{}
		controllerOptions = ControllerOptions{}
		nodeOptions       = NodeOptions{}
	)

	serverOptions.AddFlags(fs)
	c := llogApi.NewLoggingConfiguration()

	err := llogApi.AddFeatureGates(featureGate)
	if err != nil {
		llog.ErrorS(err, "failed to add feature gates")
	}

	llogApi.AddFlags(c, fs)

	if len(los.Args) > 1 && !lstr.HasPrefix(los.Args[1], "-") {
		cmd = los.Args[1]
		args = los.Args[2:]
	}

	switch cmd {
	case string(lsdriver.ControllerMode):
		controllerOptions.AddFlags(fs)

	case string(lsdriver.NodeMode):
		nodeOptions.AddFlags(fs)

	case string(lsdriver.AllMode):
		controllerOptions.AddFlags(fs)
		nodeOptions.AddFlags(fs)

	default:
		llog.Errorf("Unknown driver mode %s: Expected %s, %s, %s", cmd, lsdriver.ControllerMode, lsdriver.NodeMode, lsdriver.AllMode)
		llog.FlushAndExit(llog.ExitFlushTimeout, 0)
	}

	if err = fs.Parse(args); err != nil {
		panic(err)
	}

	if cmd != string(lsdriver.ControllerMode) {
		// nodeOptions must have been populated from the cmdline, validate them.
		if err := nodeOptions.Validate(); err != nil {
			llog.Error(err.Error())
			llog.FlushAndExit(llog.ExitFlushTimeout, 1)
		}
	}

	err = llogApi.ValidateAndApply(c, featureGate)
	if err != nil {
		llog.ErrorS(err, "failed to validate and apply logging configuration")
	}

	if *version {
		versionInfo, err := lsdriver.GetVersionJSON()
		if err != nil {
			llog.ErrorS(err, "failed to get version")
			llog.FlushAndExit(llog.ExitFlushTimeout, 1)
		}
		llog.InfoS(versionInfo)
		osExit(0)
	}

	if *toStderr {
		llog.SetOutput(los.Stderr)
	}

	config, err := getConfigFromEnv()
	if err != nil {
		llog.Errorf("Failed to get config from files: %v", err)
		llog.FlushAndExit(llog.ExitFlushTimeout, 1)
	}

	options := &Options{
		DriverMode:        lsdriver.Mode(cmd),
		ServerOptions:     &serverOptions,
		ControllerOptions: &controllerOptions,
		NodeOptions:       &nodeOptions,
		Global:            config,
	}

	return options
}

func getConfigFromEnv() (*Global, error) {
	clientID := los.Getenv("VNGCLOUD_ACCESS_KEY_ID")
	clientSecret := los.Getenv("VNGCLOUD_SECRET_ACCESS_KEY")
	identityEndpoint := los.Getenv("VNGCLOUD_IDENTITY_ENDPOINT")
	vserverEndpoint := los.Getenv("VNGCLOUD_VSERVER_ENDPOINT")

	if clientID == "" || clientSecret == "" || identityEndpoint == "" || vserverEndpoint == "" {
		return nil, lfmt.Errorf("missing required environment variables")
	}

	var cfg Global
	cfg.ClientID = clientID
	cfg.ClientSecret = clientSecret
	cfg.IdentityURL = identityEndpoint
	cfg.VServerURL = vserverEndpoint

	return &cfg, nil
}
