package httpserver

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// metricsRecorder collects OTel-compatible HTTP metrics and renders them in
// the Prometheus text exposition format, which OpenTelemetry Prometheus
// exporters scrape natively.
type metricsRecorder struct {
	mu        sync.Mutex
	counts    map[metricKey]uint64
	sums      map[metricKey]float64
	histogram map[metricKey]map[float64]uint64
	buckets   []float64
	start     time.Time
}

type metricKey struct {
	method string
	route  string
	status string
}

var defaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func newMetricsRecorder() *metricsRecorder {
	return &metricsRecorder{
		counts:    map[metricKey]uint64{},
		sums:      map[metricKey]float64{},
		histogram: map[metricKey]map[float64]uint64{},
		buckets:   defaultBuckets,
		start:     time.Now(),
	}
}

func (m *metricsRecorder) observe(method, route, status string, seconds float64) {
	key := metricKey{method: method, route: route, status: status}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[key]++
	m.sums[key] += seconds
	buckets, ok := m.histogram[key]
	if !ok {
		buckets = make(map[float64]uint64, len(m.buckets))
		for _, bound := range m.buckets {
			buckets[bound] = 0
		}
		m.histogram[key] = buckets
	}
	for _, bound := range m.buckets {
		if seconds <= bound {
			buckets[bound]++
		}
	}
}

// contentType is the Prometheus text exposition format version 0.0.4.
const contentType = "text/plain; version=0.0.4; charset=utf-8"

func (m *metricsRecorder) render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var builder strings.Builder
	builder.WriteString("# HELP modura_http_requests_total Total HTTP requests processed.\n")
	builder.WriteString("# TYPE modura_http_requests_total counter\n")
	builder.WriteString("# HELP modura_http_request_duration_seconds HTTP request latency.\n")
	builder.WriteString("# TYPE modura_http_request_duration_seconds histogram\n")
	keys := make([]metricKey, 0, len(m.counts))
	for key := range m.counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})
	for _, key := range keys {
		label := fmt.Sprintf("method=%q route=%q status=%q", key.method, key.route, key.status)
		fmt.Fprintf(&builder, "modura_http_requests_total{%s} %d\n", label, m.counts[key])
		buckets := m.histogram[key]
		for _, bound := range m.buckets {
			fmt.Fprintf(&builder, "modura_http_request_duration_seconds_bucket{%s le=%q} %d\n", label, strconv.FormatFloat(bound, 'f', -1, 64), buckets[bound])
		}
		fmt.Fprintf(&builder, "modura_http_request_duration_seconds_bucket{%s le=\"+Inf\"} %d\n", label, m.counts[key])
		fmt.Fprintf(&builder, "modura_http_request_duration_seconds_sum{%s} %f\n", label, m.sums[key])
		fmt.Fprintf(&builder, "modura_http_request_duration_seconds_count{%s} %d\n", label, m.counts[key])
	}
	seconds := time.Since(m.start).Seconds()
	builder.WriteString("# HELP modura_http_uptime_seconds Seconds since the HTTP server started collecting metrics.\n")
	builder.WriteString("# TYPE modura_http_uptime_seconds gauge\n")
	fmt.Fprintf(&builder, "modura_http_uptime_seconds %f\n", seconds)
	return builder.String()
}

func (m *metricsRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(m.render()))
	}
}
