package driver

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"k8s.io/klog/v2"
)

func InitOtelTracing() (*otlptrace.Exporter, error) {
	// Setup OTLP exporter
	ctx := context.Background()
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create the OTLP exporter: %w", err)
	}

	// Resource will auto populate spans with common attributes
	resource, err := resource.New(ctx,
		resource.WithFromEnv(), // pull attributes from OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME environment variables
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
	)
	if err != nil {
		klog.ErrorS(err, "failed to create the OTLP resource, spans will lack some metadata")
	}

	// Create a trace provider with the exporter.
	// Use propagator and sampler defined in environment variables.
	traceProvider := trace.NewTracerProvider(trace.WithBatcher(exporter), trace.WithResource(resource))

	// Register the trace provider as global.
	otel.SetTracerProvider(traceProvider)

	return exporter, nil
}
