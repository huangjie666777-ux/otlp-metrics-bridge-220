package main

import (
	"math"
	"sort"

	collectorpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func parseRequest(store *Store, request *collectorpb.ExportMetricsServiceRequest) (*pendingState, error) {
	pending := store.Begin()
	for _, resourceMetrics := range request.GetResourceMetrics() {
		instance, err := resourceInstance(resourceMetrics.GetResource())
		if err != nil {
			return nil, err
		}
		for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
			scope := ""
			if scopeMetrics.GetScope() != nil {
				scope = scopeMetrics.GetScope().GetName()
			}
			for _, metric := range scopeMetrics.GetMetrics() {
				if err := parseMetric(store, pending, instance, scope, metric); err != nil {
					return nil, err
				}
			}
		}
	}
	return pending, nil
}

func parseMetric(store *Store, pending *pendingState, instance, scope string, metric *metricpb.Metric) error {
	if !isValidMetricName(metric.GetName()) {
		return errInvalid("invalid Prometheus metric name %q", metric.GetName())
	}
	switch data := metric.GetData().(type) {
	case *metricpb.Metric_Sum:
		if data.Sum.GetAggregationTemporality() != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA || !data.Sum.GetIsMonotonic() {
			return errInvalid("metric %q must be a DELTA monotonic Sum", metric.GetName())
		}
		for _, point := range data.Sum.GetDataPoints() {
			attrs, err := parseAttributes(point.GetAttributes())
			if err != nil {
				return err
			}
			value, err := numberValue(point)
			if err != nil {
				return err
			}
			raw, err := makeRawPoint(point.GetStartTimeUnixNano(), point.GetTimeUnixNano())
			if err != nil {
				return err
			}
			raw.Value = value
			labels := baseLabels(instance, scope, attrs)
			key := makeSeriesKey(instance, scope, metric.GetName(), attrs)
			if err := store.Stage(pending, key, KindCounter, labels, raw); err != nil {
				return err
			}
		}
	case *metricpb.Metric_Histogram:
		if data.Histogram.GetAggregationTemporality() != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
			return errInvalid("metric %q must be a DELTA Histogram", metric.GetName())
		}
		for _, point := range data.Histogram.GetDataPoints() {
			attrs, err := parseAttributes(point.GetAttributes())
			if err != nil {
				return err
			}
			raw, err := parseHistogramPoint(point.GetStartTimeUnixNano(), point.GetTimeUnixNano(), point)
			if err != nil {
				return err
			}
			labels := baseLabels(instance, scope, attrs)
			key := makeSeriesKey(instance, scope, metric.GetName(), attrs)
			if err := store.Stage(pending, key, KindHistogram, labels, raw); err != nil {
				return err
			}
		}
	default:
		return errInvalid("metric %q has unsupported type; only DELTA monotonic Sum and explicit-bucket Histogram are accepted", metric.GetName())
	}
	return nil
}

func resourceInstance(resource *resourcepb.Resource) (string, error) {
	if resource == nil {
		return "", nil
	}
	for _, attr := range resource.GetAttributes() {
		if attr.GetKey() == "service.instance.id" {
			if attr.GetValue() == nil {
				return "", errInvalid("service.instance.id must be a string")
			}
			value, ok := attr.GetValue().GetValue().(*commonv1.AnyValue_StringValue)
			if !ok {
				return "", errInvalid("service.instance.id must be a string")
			}
			return value.StringValue, nil
		}
	}
	return "", nil
}

func parseAttributes(values []*commonv1.KeyValue) ([]Label, error) {
	seen := map[string]bool{}
	labels := make([]Label, 0, len(values))
	for _, pair := range values {
		if pair == nil {
			return nil, errInvalid("invalid Prometheus attribute key %q", pair.GetKey())
		}
		if !isValidLabelName(pair.GetKey()) {
			return nil, errInvalid("invalid Prometheus attribute key %q", pair.GetKey())
		}
		if pair.GetKey() == "instance" || pair.GetKey() == "scope" || pair.GetKey() == "le" {
			return nil, errInvalid("point attribute key %q is reserved", pair.GetKey())
		}
		if seen[pair.GetKey()] {
			return nil, errInvalid("duplicate point attribute key %q", pair.GetKey())
		}
		stringValue, ok := pair.GetValue().GetValue().(*commonv1.AnyValue_StringValue)
		if !ok {
			return nil, errInvalid("attribute %q must be a string", pair.GetKey())
		}
		seen[pair.GetKey()] = true
		labels = append(labels, Label{Name: pair.GetKey(), Value: stringValue.StringValue})
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
	return labels, nil
}

func stringValue(value *commonv1.AnyValue) string {
	stringValue, ok := value.GetValue().(*commonv1.AnyValue_StringValue)
	if !ok {
		return ""
	}
	return stringValue.StringValue
}

func baseLabels(instance, scope string, attrs []Label) []Label {
	labels := make([]Label, 0, len(attrs)+2)
	labels = append(labels, Label{Name: "instance", Value: instance}, Label{Name: "scope", Value: scope})
	labels = append(labels, attrs...)
	sort.SliceStable(labels[2:], func(i, j int) bool { return labels[2+i].Name < labels[2+j].Name })
	return labels
}

func numberValue(point *metricpb.NumberDataPoint) (float64, error) {
	switch value := point.GetValue().(type) {
	case *metricpb.NumberDataPoint_AsInt:
		if value.AsInt < 0 {
			return 0, errInvalid("monotonic Sum value must be non-negative")
		}
		return float64(value.AsInt), nil
	case *metricpb.NumberDataPoint_AsDouble:
		if math.IsNaN(value.AsDouble) || math.IsInf(value.AsDouble, 0) || value.AsDouble < 0 {
			return 0, errInvalid("monotonic Sum value must be finite and non-negative")
		}
		return value.AsDouble, nil
	default:
		return 0, errInvalid("NumberDataPoint must contain as_int or as_double")
	}
}

func makeRawPoint(start, end uint64) (rawPoint, error) {
	if start == 0 || end == 0 || start >= end {
		return rawPoint{}, errInvalid("time interval must use non-zero nanoseconds and start before end")
	}
	return rawPoint{Start: start, End: end}, nil
}

func parseHistogramPoint(start, end uint64, point *metricpb.HistogramDataPoint) (rawPoint, error) {
	raw, err := makeRawPoint(start, end)
	if err != nil {
		return raw, err
	}
	bounds := point.GetExplicitBounds()
	counts := point.GetBucketCounts()
	if len(bounds) == 0 || len(counts) != len(bounds)+1 {
		return raw, errInvalid("histogram must have explicit bounds and one more bucket count than bounds")
	}
	previous := math.Inf(-1)
	for _, bound := range bounds {
		if math.IsNaN(bound) || math.IsInf(bound, 0) || bound <= previous {
			return raw, errInvalid("histogram bounds must be finite and strictly increasing")
		}
		previous = bound
	}
	var total uint64
	for _, count := range counts {
		if total > math.MaxUint64-count {
			return raw, errInvalid("histogram bucket counts overflow")
		}
		total += count
	}
	if total != point.GetCount() {
		return raw, errInvalid("histogram bucket counts must sum to count")
	}
	if point.Sum == nil {
		return raw, errInvalid("histogram sum is required")
	}
	if math.IsNaN(point.GetSum()) || math.IsInf(point.GetSum(), 0) || point.GetSum() < 0 {
		return raw, errInvalid("histogram sum must be finite and non-negative")
	}
	raw.Count = point.GetCount()
	raw.Sum = point.GetSum()
	raw.HasSum = true
	raw.Bounds = append([]float64(nil), bounds...)
	raw.Counts = append([]uint64(nil), counts...)
	return raw, nil
}
