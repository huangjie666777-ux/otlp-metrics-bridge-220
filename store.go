package main

import (
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type MetricKind int

const (
	KindCounter MetricKind = iota + 1
	KindHistogram
)

type Label struct {
	Name  string
	Value string
}

type SeriesKey struct {
	Instance   string
	Scope      string
	Metric     string
	Attributes string
}

type rawPoint struct {
	Start  uint64
	End    uint64
	Value  float64
	Count  uint64
	Sum    float64
	HasSum bool
	Bounds []float64
	Counts []uint64
}

type Series struct {
	Metric    string
	Kind      MetricKind
	Labels    []Label
	Bounds    []float64
	Value     float64
	Count     uint64
	Sum       float64
	Counts    []uint64
	Last      rawPoint
	HasPoint  bool
	UpdatedAt time.Time
}

type SnapshotSeries struct {
	Metric string
	Kind   MetricKind
	Labels []Label
	Value  float64
	Count  uint64
	Sum    float64
	Bounds []float64
	Counts []uint64
}

type Snapshot struct {
	Series []SnapshotSeries
}

type Store struct {
	mu         sync.RWMutex
	now        func() time.Time
	maxSeries  int
	idleExpiry time.Duration
	series     map[SeriesKey]*Series
	kinds      map[string]MetricKind
}

func NewStore(maxSeries int, idleExpireSecs int64, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{
		now:        now,
		maxSeries:  maxSeries,
		idleExpiry: time.Duration(idleExpireSecs) * time.Second,
		series:     make(map[SeriesKey]*Series),
		kinds:      make(map[string]MetricKind),
	}
}

type pendingUpdate struct {
	key    SeriesKey
	kind   MetricKind
	labels []Label
	point  rawPoint
}

type pendingState struct {
	updates  []pendingUpdate
	created  map[SeriesKey]*Series
	changed  map[SeriesKey]*Series
	original map[SeriesKey]*Series
	kinds    map[string]MetricKind
}

func (s *Store) Begin() *pendingState {
	kinds := make(map[string]MetricKind, len(s.kinds))
	s.mu.RLock()
	for name, kind := range s.kinds {
		kinds[name] = kind
	}
	s.mu.RUnlock()
	return &pendingState{created: map[SeriesKey]*Series{}, changed: map[SeriesKey]*Series{}, original: map[SeriesKey]*Series{}, kinds: kinds}
}

func (s *Store) Stage(p *pendingState, key SeriesKey, kind MetricKind, labels []Label, point rawPoint) error {
	if existing, ok := p.kinds[key.Metric]; ok && existing != kind {
		return errInvalid("metric %q has conflicting types", key.Metric)
	}
	current := p.created[key]
	if current == nil {
		current = p.changed[key]
	}
	if current == nil {
		s.mu.RLock()
		original := s.series[key]
		s.mu.RUnlock()
		if original != nil {
			copied := cloneSeries(original)
			current = &copied
			p.changed[key] = current
			p.original[key] = original
		} else {
			current = &Series{Metric: key.Metric, Kind: kind, Labels: append([]Label(nil), labels...), UpdatedAt: s.now()}
			p.created[key] = current
		}
	}
	if current.Kind != kind {
		return errInvalid("metric %q has conflicting types", key.Metric)
	}
	if err := applyPoint(current, point, s.now()); err != nil {
		return err
	}
	p.kinds[key.Metric] = kind
	p.updates = append(p.updates, pendingUpdate{key: key, kind: kind, labels: labels, point: point})
	return nil
}

func (s *Store) Commit(p *pendingState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked()
	if len(s.series)+len(p.created) > s.maxSeries {
		return errInvalid("series limit %d exceeded", s.maxSeries)
	}
	for name, kind := range p.kinds {
		if existing, ok := s.kinds[name]; ok && existing != kind {
			return errInvalid("metric %q has conflicting types", name)
		}
		s.kinds[name] = kind
	}
	for key, created := range p.created {
		if _, exists := s.series[key]; exists {
			return errInvalid("concurrent series creation for %q", key.Metric)
		}
		s.series[key] = created
	}
	for key, changed := range p.changed {
		current, exists := s.series[key]
		if !exists {
			return errInvalid("series expired during commit")
		}
		if current != p.original[key] {
			return errInvalid("concurrent update conflict for metric %q", key.Metric)
		}
		s.series[key] = changed
	}
	return nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	s.expireLocked()
	all := make([]*Series, 0, len(s.series))
	for _, item := range s.series {
		all = append(all, item)
	}
	s.mu.Unlock()

	sort.Slice(all, func(i, j int) bool {
		if all[i].Metric != all[j].Metric {
			return all[i].Metric < all[j].Metric
		}
		return labelSignature(all[i].Labels) < labelSignature(all[j].Labels)
	})
	out := make([]SnapshotSeries, 0, len(all))
	for _, item := range all {
		out = append(out, SnapshotSeries{Metric: item.Metric, Kind: item.Kind, Labels: append([]Label(nil), item.Labels...), Value: item.Value, Count: item.Count, Sum: item.Sum, Bounds: append([]float64(nil), item.Bounds...), Counts: append([]uint64(nil), item.Counts...)})
	}
	return Snapshot{Series: out}
}

func (s *Store) expireLocked() {
	if s.idleExpiry <= 0 {
		return
	}
	cutoff := s.now().Add(-s.idleExpiry)
	active := map[string]bool{}
	for key, item := range s.series {
		if item.UpdatedAt.Before(cutoff) {
			delete(s.series, key)
			continue
		}
		active[key.Metric] = true
	}
	for name := range s.kinds {
		if !active[name] {
			delete(s.kinds, name)
		}
	}
}

func cloneSeries(input *Series) Series {
	return Series{Metric: input.Metric, Kind: input.Kind, Labels: append([]Label(nil), input.Labels...), Bounds: append([]float64(nil), input.Bounds...), Value: input.Value, Count: input.Count, Sum: input.Sum, Counts: append([]uint64(nil), input.Counts...), Last: cloneRaw(input.Last), HasPoint: input.HasPoint, UpdatedAt: input.UpdatedAt}
}

func cloneRaw(input rawPoint) rawPoint {
	return rawPoint{Start: input.Start, End: input.End, Value: input.Value, Count: input.Count, Sum: input.Sum, HasSum: input.HasSum, Bounds: append([]float64(nil), input.Bounds...), Counts: append([]uint64(nil), input.Counts...)}
}

func applyPoint(series *Series, point rawPoint, now time.Time) error {
	if series.HasPoint {
		sameWindow := point.Start == series.Last.Start && point.End == series.Last.End
		if sameWindow {
			if reflect.DeepEqual(point, series.Last) {
				return nil
			}
			return errInvalid("different content retransmitted for interval %d-%d", point.Start, point.End)
		}
		if point.Start < series.Last.End || point.End <= series.Last.End {
			return errInvalid("overlapping or backwards interval %d-%d after %d-%d", point.Start, point.End, series.Last.Start, series.Last.End)
		}
		if point.Start > series.Last.End {
			resetSeries(series)
		}
	}
	switch series.Kind {
	case KindCounter:
		value, err := addFloat(series.Value, point.Value)
		if err != nil {
			return err
		}
		series.Value = value
	case KindHistogram:
		if len(series.Bounds) == 0 {
			series.Bounds = append([]float64(nil), point.Bounds...)
			series.Counts = make([]uint64, len(point.Counts))
		} else if !reflect.DeepEqual(series.Bounds, point.Bounds) {
			return errInvalid("histogram boundaries changed for metric %q", series.Metric)
		}
		count, err := addUint(series.Count, point.Count)
		if err != nil {
			return err
		}
		sum, err := addFloat(series.Sum, point.Sum)
		if err != nil {
			return err
		}
		counts := make([]uint64, len(series.Counts))
		for i := range counts {
			counts[i], err = addUint(series.Counts[i], point.Counts[i])
			if err != nil {
				return err
			}
		}
		series.Count, series.Sum, series.Counts = count, sum, counts
	}
	series.Last = cloneRaw(point)
	series.HasPoint = true
	series.UpdatedAt = now
	return nil
}

func resetSeries(series *Series) {
	series.Value = 0
	series.Count = 0
	series.Sum = 0
	for i := range series.Counts {
		series.Counts[i] = 0
	}
}

func addFloat(left, right float64) (float64, error) {
	if right < 0 || math.IsNaN(right) || math.IsInf(right, 0) || left > math.MaxFloat64-right {
		return 0, errInvalid("floating-point accumulation overflow or invalid value")
	}
	return left + right, nil
}

func addUint(left, right uint64) (uint64, error) {
	if left > math.MaxUint64-right {
		return 0, errInvalid("integer accumulation overflow")
	}
	return left + right, nil
}

func makeSeriesKey(instance, scope, metricName string, attrs []Label) SeriesKey {
	parts := make([]string, 0, len(attrs)*2)
	for _, attr := range attrs {
		parts = append(parts, attr.Name, "=\x00", attr.Value, "\x00")
	}
	return SeriesKey{Instance: instance, Scope: scope, Metric: metricName, Attributes: strings.Join(parts, "")}
}

func labelSignature(labels []Label) string {
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, label.Name+"="+label.Value)
	}
	return strings.Join(parts, "\x00")
}
