package driver

import (
	"context"
	"fmt"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
	"github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"net"
	"time"
)

const (
	DriverName = "bs.csi.vngcloud.vn"
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

type DriverOptions struct {
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
}

func NewDriver(options ...func(*DriverOptions)) (*Driver, error) {
	klog.InfoS("Driver Information", "Driver", DriverName, "Version", driverVersion)

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

func (d *Driver) Run() error {
	scheme, addr, err := util.ParseEndpoint(d.options.endpoint)
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
	if d.options.otelTracing {
		opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	}
	d.srv = grpc.NewServer(opts...)

	csi.RegisterIdentityServer(d.srv, d)

	switch d.options.mode {
	case ControllerMode:
		csi.RegisterControllerServer(d.srv, d)
		rpc.RegisterModifyServer(d.srv, d)
	case NodeMode:
		csi.RegisterNodeServer(d.srv, d)
	case AllMode:
		csi.RegisterControllerServer(d.srv, d)
		csi.RegisterNodeServer(d.srv, d)
		rpc.RegisterModifyServer(d.srv, d)
	default:
		return fmt.Errorf("unknown mode: %s", d.options.mode)
	}

	klog.V(4).InfoS("Listening for connections", "address", listener.Addr())
	return d.srv.Serve(listener)
}
