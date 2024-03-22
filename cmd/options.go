package main

import (
	"fmt"
	flag "github.com/spf13/pflag"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver"
	"gopkg.in/gcfg.v1"
	"k8s.io/component-base/featuregate"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	"os"
	"strings"
	"time"
)

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

type Config struct {
	Global Global
}

type Option struct{}

type Global struct {
	IdentityURL  string `gcfg:"identity-url" mapstructure:"identity-url" name:"identity-url"`
	VServerURL   string `gcfg:"vserver-url" mapstructure:"vserver-url" name:"vserver-url"`
	ClientID     string `gcfg:"client-id" mapstructure:"client-id" name:"client-id"`
	ClientSecret string `gcfg:"client-secret" mapstructure:"client-secret" name:"client-secret"`
}

type ServerOptions struct {
	Endpoint          string
	HttpEndpoint      string
	EnableOtelTracing bool
	ConfigFilePath    string
}

func (s *ServerOptions) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.Endpoint, "endpoint", driver.DefaultCSIEndpoint, "Endpoint for the CSI driver server")
	fs.StringVar(&s.HttpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for metrics will listen (example: `:8080`). The default is empty string, which means the server is disabled.")
	fs.BoolVar(&s.EnableOtelTracing, "enable-otel-tracing", false, "To enable opentelemetry tracing for the driver. The tracing is disabled by default. Configure the exporter endpoint with OTEL_EXPORTER_OTLP_ENDPOINT and other env variables, see https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/#general-sdk-configuration.")
	fs.StringVar(&s.ConfigFilePath, "config-file-path", "/etc/config/vcontainer-csi-config.conf", "Path to the configuration file")
}

type ControllerOptions struct {
	ModifyVolumeRequestHandlerTimeout time.Duration
}

func (s *ControllerOptions) AddFlags(fs *flag.FlagSet) {
	fs.DurationVar(&s.ModifyVolumeRequestHandlerTimeout, "modify-volume-request-handler-timeout", driver.DefaultModifyVolumeRequestHandlerTimeout, "Timeout for the window in which volume modification calls must be received in order for them to coalesce into a single volume modification call to AWS. This must be lower than the csi-resizer and volumemodifier timeouts")

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

	if serverOptions.ConfigFilePath == "" {
		klog.Errorf("Config file path is empty")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	config, err := getConfigFromFiles([]string{serverOptions.ConfigFilePath})
	if err != nil {
		klog.Errorf("Failed to get config from files: %v", err)
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	options := &Options{
		DriverMode:        driver.Mode(cmd),
		ServerOptions:     &serverOptions,
		ControllerOptions: &controllerOptions,
		NodeOptions:       &nodeOptions,
		Global:            &config.Global,
	}

	return options
}

func getConfigFromFiles(configFiles []string) (*Config, error) {
	var cfg Config

	// Read all specified config files in order. Values from later config files
	// will overwrite values from earlier ones.
	for _, configFilePath := range configFiles {
		config, err := os.Open(configFilePath)
		if err != nil {
			klog.Errorf("failed to open VngCloud configuration file: %v", err)
			return nil, err
		}

		defer config.Close()

		err = gcfg.FatalOnly(gcfg.ReadInto(&cfg, config))
		if err != nil {
			klog.Errorf("failed to read VngCloud configuration file: %v", err)
			return nil, err
		}
	}

	return &cfg, nil
}
