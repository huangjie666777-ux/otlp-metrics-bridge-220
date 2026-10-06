package main

import (
	"strings"
	"testing"
	"time"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func TestCounterAccumulationRetransmitAndReset(t *testing.T) {
	clock := time.Unix(100, 0)
	store := NewStore(10, 0, func() time.Time { return clock })
	submit := func(start, end uint64, value float64) {
		t.Helper()
		pending, err := parseRequest(store, counterRequest("requests", start, end, value))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Commit(pending); err != nil {
			t.Fatal(err)
		}
	}
	submit(100, 200, 3)
	submit(200, 300, 4)
	assertCounter(t, store, 7)
	clock = clock.Add(time.Second)
	submit(200, 300, 4)
	assertCounter(t, store, 7)
	submit(400, 500, 2)
	assertCounter(t, store, 2)
}

func TestHistogramBucketsAndEscapedLabels(t *testing.T) {
	store := NewStore(10, 0, func() time.Time { return time.Unix(100, 0) })
	pending, err := parseRequest(store, histogramRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(pending); err != nil {
		t.Fatal(err)
	}
	output := RenderPrometheus(store.Snapshot())
	expectations := []string{
		`# TYPE latency histogram`,
		`latency_bucket{instance="host\\a",scope="svc",path="a\"b",le="0.5"} 1`,
		`latency_bucket{instance="host\\a",scope="svc",path="a\"b",le="1"} 2`,
		`latency_bucket{instance="host\\a",scope="svc",path="a\"b",le="+Inf"} 3`,
		`latency_sum{instance="host\\a",scope="svc",path="a\"b"} 1.5`,
		`latency_count{instance="host\\a",scope="svc",path="a\"b"} 3`,
	}
	for _, expected := range expectations {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q in:\n%s", expected, output)
		}
	}
}

func TestInvalidBatchIsAtomic(t *testing.T) {
	store := NewStore(10, 0, func() time.Time { return time.Unix(100, 0) })
	request := counterRequest("requests", 100, 200, 1)
	request.ResourceMetrics[0].ScopeMetrics[0].Metrics = append(
		request.ResourceMetrics[0].ScopeMetrics[0].Metrics,
		&metricpb.Metric{Name: "requests", Data: &metricpb.Metric_Gauge{Gauge: &metricpb.Gauge{}}},
	)
	if _, err := parseRequest(store, request); err == nil {
		t.Fatal("expected mixed metric types to be rejected")
	}
	if output := RenderPrometheus(store.Snapshot()); output != "" {
		t.Fatalf("invalid batch left state: %s", output)
	}
}

func TestSeriesLimitAndIdleExpiry(t *testing.T) {
	clock := time.Unix(100, 0)
	store := NewStore(1, 10, func() time.Time { return clock })
	pending, err := parseRequest(store, counterRequest("one", 100, 200, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(pending); err != nil {
		t.Fatal(err)
	}
	second := counterRequest("two", 100, 200, 1)
	second.ResourceMetrics[0].Resource.Attributes[0].Value = &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "other"}}
	pending, err = parseRequest(store, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(pending); err == nil {
		t.Fatal("expected series limit rejection")
	}
	clock = clock.Add(11 * time.Second)
	if output := RenderPrometheus(store.Snapshot()); strings.Contains(output, "one_total") {
		t.Fatalf("idle series was not expired: %s", output)
	}
}

func assertCounter(t *testing.T, store *Store, expected float64) {
	t.Helper()
	snapshot := store.Snapshot()
	if len(snapshot.Series) != 1 {
		t.Fatalf("got %d series, want 1", len(snapshot.Series))
	}
	if snapshot.Series[0].Value != expected {
		t.Fatalf("got value %v, want %v", snapshot.Series[0].Value, expected)
	}
}

func counterRequest(name string, start, end uint64, value float64) *collectorpb.ExportMetricsServiceRequest {
	request := &collectorpb.ExportMetricsServiceRequest{}
	resourceMetrics := &metricpb.ResourceMetrics{
		Resource: &resourcepb.Resource{Attributes: []*commonv1.KeyValue{stringAttr("service.instance.id", "host")}},
	}
	scopeMetrics := &metricpb.ScopeMetrics{Scope: &commonv1.InstrumentationScope{Name: "svc"}}
	scopeMetrics.Metrics = []*metricpb.Metric{{
		Name: name,
		Data: &metricpb.Metric_Sum{Sum: &metricpb.Sum{
			AggregationTemporality: metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			IsMonotonic:            true,
			DataPoints: []*metricpb.NumberDataPoint{{
				StartTimeUnixNano: start,
				TimeUnixNano:      end,
				Value:             &metricpb.NumberDataPoint_AsDouble{AsDouble: value},
			}},
		}},
	}}
	resourceMetrics.ScopeMetrics = []*metricpb.ScopeMetrics{scopeMetrics}
	request.ResourceMetrics = []*metricpb.ResourceMetrics{resourceMetrics}
	return request
}

func histogramRequest() *collectorpb.ExportMetricsServiceRequest {
	request := &collectorpb.ExportMetricsServiceRequest{}
	resourceMetrics := &metricpb.ResourceMetrics{
		Resource: &resourcepb.Resource{Attributes: []*commonv1.KeyValue{stringAttr("service.instance.id", "host\\a")}},
	}
	scopeMetrics := &metricpb.ScopeMetrics{Scope: &commonv1.InstrumentationScope{Name: "svc"}}
	sumValue := 1.5
	scopeMetrics.Metrics = []*metricpb.Metric{{
		Name: "latency",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			AggregationTemporality: metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
			DataPoints: []*metricpb.HistogramDataPoint{{
				Attributes:        []*commonv1.KeyValue{stringAttr("path", "a\"b")},
				StartTimeUnixNano: 100,
				TimeUnixNano:      200,
				Count:             3,
				Sum:               &sumValue,
				BucketCounts:      []uint64{1, 1, 1},
				ExplicitBounds:    []float64{0.5, 1},
			}},
		}},
	}}
	resourceMetrics.ScopeMetrics = []*metricpb.ScopeMetrics{scopeMetrics}
	request.ResourceMetrics = []*metricpb.ResourceMetrics{resourceMetrics}
	return request
}

func stringAttr(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}},
	}
}
