package driver

import (
	"context"
	"fmt"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"net"
	"time"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
)

// Mode is the operating mode of the CSI driver.
type Mode string

const (
	// ControllerMode is the mode that only starts the controller service.
	ControllerMode Mode = "controller"
	// NodeMode is the mode that only starts the node service.
	NodeMode Mode = "node"
	// AllMode is the mode that only starts both the controller and the node service.
	AllMode Mode = "all"
)

type Driver struct {
	controllerService
	nodeService

	srv     *grpc.Server
	options *DriverOptions
}

type DriverOptions struct { // nolint: maligned
	endpoint                          string
	mode                              Mode
	otelTracing                       bool
	userAgentExtra                    string
	modifyVolumeRequestHandlerTimeout time.Duration
	clientID                          string
	clientSecret                      string
	identityURL                       string
	vServerURL                        string
	batching                          bool
	clusterID                         string
	cacheUri                          string
	alertChannel                      string
	alertChannelSize                  int
	tagKeyLength                      int
	tagValueLength                    int
}

func NewDriver(options ...func(*DriverOptions)) (*Driver, error) {
	klog.InfoS("[INFO] - NewDriver: Driver Information", "Driver", DriverName, "Version", driverVersion)

	driverOptions := DriverOptions{
		endpoint:                          DefaultCSIEndpoint,
		mode:                              AllMode,
		modifyVolumeRequestHandlerTimeout: DefaultModifyVolumeRequestHandlerTimeout,
	}

	for _, option := range options {
		option(&driverOptions)
	}

	if err := ValidateDriverOptions(&driverOptions); err != nil {
		return nil, fmt.Errorf("Invalid driver options: %w", err)
	}

	driver := Driver{
		options: &driverOptions,
	}

	switch driverOptions.mode {
	case ControllerMode:
		driver.controllerService = newControllerService(&driverOptions)
	case NodeMode:
		driver.nodeService = newNodeService(&driverOptions)
	case AllMode:
		driver.controllerService = newControllerService(&driverOptions)
		driver.nodeService = newNodeService(&driverOptions)
	default:
		return nil, fmt.Errorf("unknown mode: %s", driverOptions.mode)
	}

	return &driver, nil
}

func WithEndpoint(endpoint string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.endpoint = endpoint
	}
}

func WithMode(mode Mode) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.mode = mode
	}
}

func WithAlertChannel(alertChannel string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.alertChannel = alertChannel
	}
}

func WithAlertChannelSize(alertChannelSize int) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.alertChannelSize = alertChannelSize
	}
}

func WithCacheUri(cacheUri string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.cacheUri = cacheUri
	}
}

func WithOtelTracing(enableOtelTracing bool) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.otelTracing = enableOtelTracing
	}
}

func WithModifyVolumeRequestHandlerTimeout(timeout time.Duration) func(*DriverOptions) {
	return func(o *DriverOptions) {
		if timeout == 0 {
			return
		}
		o.modifyVolumeRequestHandlerTimeout = timeout
	}
}

func WithClientID(clientID string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.clientID = clientID
	}
}

func WithClientSecret(clientSecret string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.clientSecret = clientSecret
	}
}

func WithIdentityURL(identityURL string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.identityURL = identityURL
	}
}

func WithClusterID(pclusterID string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.clusterID = pclusterID
	}
}

func WithTagKeyLength(ptkl int) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.tagKeyLength = ptkl
	}
}

func WithTagValueLength(ptvl int) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.tagValueLength = ptvl
	}
}

func WithUserAgentExtra(userAgentExtra string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.userAgentExtra = userAgentExtra
	}
}

func WithVServerURL(vServerURL string) func(*DriverOptions) {
	return func(o *DriverOptions) {
		o.vServerURL = vServerURL
	}
}

func (s *Driver) Run() error {
	scheme, addr, err := util.ParseEndpoint(s.options.endpoint)
	if err != nil {
		return err
	}

	listener, err := net.Listen(scheme, addr)
	if err != nil {
		return err
	}

	logErr := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.ErrorS(err, "GRPC error")
		}
		return resp, err
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logErr),
	}
	if s.options.otelTracing {
		opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	}
	s.srv = grpc.NewServer(opts...)

	csi.RegisterIdentityServer(s.srv, s)

	switch s.options.mode {
	case ControllerMode:
		csi.RegisterControllerServer(s.srv, s)
		rpc.RegisterModifyServer(s.srv, s)
	case NodeMode:
		csi.RegisterNodeServer(s.srv, s)
	case AllMode:
		csi.RegisterControllerServer(s.srv, s)
		csi.RegisterNodeServer(s.srv, s)
		rpc.RegisterModifyServer(s.srv, s)
	default:
		return fmt.Errorf("unknown mode: %s", s.options.mode)
	}

	klog.V(4).InfoS("[INFO] - Run: Listening for connections", "address", listener.Addr())
	return s.srv.Serve(listener)
}

func (s *DriverOptions) GetTagKeyLength() int {
	return s.tagKeyLength
}

func (s *DriverOptions) GetTagValueLength() int {
	return s.tagValueLength
}
