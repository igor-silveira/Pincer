package retry

import (
	"context"

	"github.com/igorsilveira/pincer/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

func EmitRetrySpan(ctx context.Context, rec AttemptRecord) {
	ctx, span := telemetry.StartSpan(ctx, "agent.retry",
		attribute.String("strategy", rec.Strategy),
		attribute.Int("attempt", rec.Attempt),
		attribute.String("error", rec.Error),
		attribute.Int64("duration_ms", rec.Duration.Milliseconds()),
	)
	defer span.End()
	_ = ctx
}
