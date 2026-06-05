/*
  Copyright 2026.

  Licensed under the Apache License, Version 2.0 (the "License").
*/

// Package tracing wires the global OpenTelemetry tracer to an OTLP gRPC
// collector (Jaeger by default). Init is meant to be called once from
// main; the returned shutdown function flushes pending spans on exit.
package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Init configures the global OpenTelemetry tracer provider to export
// spans via OTLP/gRPC to the given collector endpoint (e.g.
// "localhost:4317" for a local Jaeger all-in-one).
//
// serviceName is attached as the service identity on every span produced
// by this process (e.g. "agentmesh-control-plane", "sidecar-search-1").
//
// Returns a shutdown function that flushes the batch processor and
// closes the exporter. Defer it from main.
func Init(ctx context.Context, serviceName, collectorEndpoint string) (func(context.Context) error, error) {
	// Plaintext gRPC connection to the collector. mTLS comes in Phase 2.
	conn, err := grpc.NewClient(
		collectorEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial OTLP collector: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	// W3C TraceContext is the standard cross-process propagator; Baggage
	// carries arbitrary key-value pairs across hops.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
