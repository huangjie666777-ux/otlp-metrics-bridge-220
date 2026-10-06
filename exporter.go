package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

func RenderPrometheus(snapshot Snapshot) string {
	var builder strings.Builder
	metricsByName := map[string][]SnapshotSeries{}
	for _, series := range snapshot.Series {
		metricsByName[series.Metric] = append(metricsByName[series.Metric], series)
	}
	names := make([]string, 0, len(metricsByName))
	for name := range metricsByName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		items := metricsByName[name]
		switch items[0].Kind {
		case KindCounter:
			fmt.Fprintf(&builder, "# TYPE %s_total counter\n", name)
			for _, item := range items {
				fmt.Fprintf(&builder, "%s_total%s %s\n", name, renderLabels(item.Labels, nil), formatFloat(item.Value))
			}
		case KindHistogram:
			fmt.Fprintf(&builder, "# TYPE %s histogram\n", name)
			for _, item := range items {
				cumulative := uint64(0)
				for i, bound := range item.Bounds {
					cumulative += item.Counts[i]
					fmt.Fprintf(&builder, "%s_bucket%s %d\n", name, renderLabels(item.Labels, []Label{{Name: "le", Value: formatFloat(bound)}}), cumulative)
				}
				cumulative += item.Counts[len(item.Counts)-1]
				fmt.Fprintf(&builder, "%s_bucket%s %d\n", name, renderLabels(item.Labels, []Label{{Name: "le", Value: "+Inf"}}), cumulative)
				fmt.Fprintf(&builder, "%s_sum%s %s\n", name, renderLabels(item.Labels, nil), formatFloat(item.Sum))
				fmt.Fprintf(&builder, "%s_count%s %d\n", name, renderLabels(item.Labels, nil), item.Count)
			}
		}
	}
	return builder.String()
}

func renderLabels(base []Label, extra []Label) string {
	labels := append(append([]Label(nil), base...), extra...)
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, label.Name+"=\""+escapeLabelValue(label.Value)+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

func formatFloat(value float64) string {
	if math.IsInf(value, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}
