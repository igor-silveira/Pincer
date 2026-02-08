package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var Metrics = struct {
	RequestsTotal     *prometheus.CounterVec
	RequestDuration   *prometheus.HistogramVec
	TokensUsed        *prometheus.CounterVec
	ToolExecutions    *prometheus.CounterVec
	ToolDuration      *prometheus.HistogramVec
	ActiveSessions    prometheus.Gauge
	ActiveConnections prometheus.Gauge
	ErrorsTotal       *prometheus.CounterVec
	LLMRequestsTotal  *prometheus.CounterVec
	LLMLatency        *prometheus.HistogramVec
}{
	RequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pincer",
		Name:      "requests_total",
		Help:      "Total number of requests by type and status.",
	}, []string{"type", "status"}),

	RequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "pincer",
		Name:      "request_duration_seconds",
		Help:      "Request duration in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"type"}),

	TokensUsed: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pincer",
		Name:      "tokens_used_total",
		Help:      "Total tokens consumed by direction (input/output) and model.",
	}, []string{"direction", "model"}),

	ToolExecutions: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pincer",
		Name:      "tool_executions_total",
		Help:      "Total tool executions by tool name and status.",
	}, []string{"tool", "status"}),

	ToolDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "pincer",
		Name:      "tool_duration_seconds",
		Help:      "Tool execution duration in seconds.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60},
	}, []string{"tool"}),

	ActiveSessions: promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pincer",
		Name:      "active_sessions",
		Help:      "Number of currently active sessions.",
	}),

	ActiveConnections: promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "pincer",
		Name:      "active_websocket_connections",
		Help:      "Number of active WebSocket connections.",
	}),

	ErrorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pincer",
		Name:      "errors_total",
		Help:      "Total errors by component.",
	}, []string{"component"}),

	LLMRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "pincer",
		Name:      "llm_requests_total",
		Help:      "Total LLM API requests by provider and model.",
	}, []string{"provider", "model"}),

	LLMLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "pincer",
		Name:      "llm_latency_seconds",
		Help:      "LLM request latency in seconds (time to first token).",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	}, []string{"provider", "model"}),
}
