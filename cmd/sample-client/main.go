package main

import (
	"log"
	"os"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func main() {
	output := "sample.pb"
	if len(os.Args) > 1 {
		output = os.Args[1]
	}
	histogramSum := 2.0
	request := &collectorpb.ExportMetricsServiceRequest{}
	resourceMetrics := &metricpb.ResourceMetrics{
		Resource: &resourcepb.Resource{Attributes: []*commonv1.KeyValue{
			{Key: "service.instance.id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "demo-instance"}}},
		}},
	}
	scopeMetrics := &metricpb.ScopeMetrics{Scope: &commonv1.InstrumentationScope{Name: "demo"}}
	scopeMetrics.Metrics = []*metricpb.Metric{
		{
			Name: "requests",
			Data: &metricpb.Metric_Sum{Sum: &metricpb.Sum{
				AggregationTemporality: metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
				IsMonotonic:            true,
				DataPoints: []*metricpb.NumberDataPoint{{
					Attributes:        []*commonv1.KeyValue{attr("route", "/hello")},
					StartTimeUnixNano: 1_700_000_000_000_000_000,
					TimeUnixNano:      1_700_000_001_000_000_000,
					Value:             &metricpb.NumberDataPoint_AsInt{AsInt: 7},
				}},
			}},
		},
		{
			Name: "latency",
			Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
				AggregationTemporality: metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA,
				DataPoints: []*metricpb.HistogramDataPoint{{
					Attributes:        []*commonv1.KeyValue{attr("route", "/hello")},
					StartTimeUnixNano: 1_700_000_000_000_000_000,
					TimeUnixNano:      1_700_000_001_000_000_000,
					Count:             3,
					Sum:               &histogramSum,
					BucketCounts:      []uint64{1, 1, 1},
					ExplicitBounds:    []float64{0.1, 1},
				}},
			}},
		},
	}
	resourceMetrics.ScopeMetrics = []*metricpb.ScopeMetrics{scopeMetrics}
	request.ResourceMetrics = []*metricpb.ResourceMetrics{resourceMetrics}

	payload, err := proto.Marshal(request)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(output, payload, 0o644); err != nil {
		log.Fatal(err)
	}
}

func attr(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}},
	}
}
