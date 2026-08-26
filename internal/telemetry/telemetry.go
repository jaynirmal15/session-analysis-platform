package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/jaynirmal15/session-analysis-platform/internal/config"
)

// ShutdownFunc flushes and releases every provider installed by Setup. It is
// safe to call once; callers should give it its own timeout, separate from the
// context that triggered shutdown.
type ShutdownFunc func(context.Context) error

// Setup installs global tracer and meter providers exporting over OTLP/gRPC to
// the collector named in cfg, and starts Go runtime instrumentation.
//
// On error, any provider already installed is shut down before returning, so a
// failed Setup leaves no exporters running.
func Setup(ctx context.Context, cfg config.Service) (ShutdownFunc, error) {
	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	var shutdowns []func(context.Context) error
	unwind := func(ctx context.Context) error {
		var errs []error
		// Shut down in reverse installation order.
		for i := len(shutdowns) - 1; i >= 0; i-- {
			errs = append(errs, shutdowns[i](ctx))
		}
		return errors.Join(errs...)
	}

	traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
	}
	traceExp, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: otlp trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExp),
	)
	shutdowns = append(shutdowns, tp.Shutdown)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
	}
	metricExp, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("telemetry: otlp metric exporter: %w", err), unwind(ctx))
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			metricExp,
			sdkmetric.WithInterval(15*time.Second),
		)),
	)
	shutdowns = append(shutdowns, mp.Shutdown)
	otel.SetMeterProvider(mp)

	// Go runtime metrics give the local stack real data to display before any
	// domain metric exists, which is how the compose wiring gets verified.
	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, errors.Join(fmt.Errorf("telemetry: start runtime instrumentation: %w", err), unwind(ctx))
	}

	return unwind, nil
}

func newResource(ctx context.Context, cfg config.Service) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.Name),
			semconv.ServiceVersion(cfg.Version),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
}
