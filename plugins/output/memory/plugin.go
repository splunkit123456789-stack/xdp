package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
	"xdp/pkg/search/splstats"
)

type Output struct {
	index string
}

type SearchQuery struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
}

type StatsQuery struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
	Stats     splstats.Query
}

type TimelineQuery struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Interval  string
	Location  *time.Location
}

type TimelineBucket struct {
	Start time.Time
	Count int
}

type IndexInfo struct {
	IndexName       string    `json:"index_name"`
	Rows            int       `json:"rows"`
	LatestEventTime time.Time `json:"latest_event_time,omitempty"`
	StorageBytes    int       `json:"storage_bytes"`
	TTLDays         int       `json:"ttl_days"`
}

var defaultStore = &Store{events: []*event.Event{}}

func New() *Output {
	return &Output{}
}

func NewStore() *Store {
	return &Store{events: []*event.Event{}}
}

func DefaultStore() *Store {
	return defaultStore
}

func (o *Output) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:    "memory-output",
		Name:    "Memory Output",
		Type:    plugin.TypeOutput,
		Version: "1.0.0",
		Runtime: "go",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"index": map[string]any{"type": "string", "default": "${metadata.index}"},
			},
		},
	}
}

func (o *Output) Validate(config map[string]any) error {
	_, err := parseConfig(config)
	return err
}
func (o *Output) Init(ctx plugin.InitContext, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	o.index = cfg.index
	return nil
}
func (o *Output) Close() error { return nil }

func (o *Output) Write(ctx context.Context, batch *plugin.EventBatch) error {
	for _, e := range batch.Events {
		if o.index != "" && o.index != "${metadata.index}" {
			e.Metadata["index"] = o.index
		}
		defaultStore.Append(e)
	}
	return nil
}

func Register(reg *plugin.Registry) error {
	output := New()
	return reg.Register(output.Metadata(), func() any { return New() })
}

type Store struct {
	mu     sync.RWMutex
	events []*event.Event
}

type outputConfig struct {
	index string
}

func parseConfig(config map[string]any) (outputConfig, error) {
	cfg := outputConfig{index: "${metadata.index}"}
	for key, value := range config {
		text, ok := value.(string)
		if !ok {
			return cfg, fmt.Errorf("%s must be a string", key)
		}
		switch key {
		case "index":
			cfg.index = text
		}
	}
	return cfg, nil
}

func (s *Store) Append(e *event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = []*event.Event{}
}

func (s *Store) Search(query SearchQuery) []*event.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched := []*event.Event{}
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	for _, e := range s.events {
		if matchesQuery(e, query.Index, query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime) {
			matched = append(matched, e)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if !matched[i].EventTime.Equal(matched[j].EventTime) {
			return matched[i].EventTime.After(matched[j].EventTime)
		}
		return matched[i].EventID > matched[j].EventID
	})
	if offset >= len(matched) {
		return []*event.Event{}
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end]
}

func (s *Store) Count(query SearchQuery) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, e := range s.events {
		if matchesQuery(e, query.Index, query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime) {
			total++
		}
	}
	return total
}

func (s *Store) Timeline(query TimelineQuery) []TimelineBucket {
	s.mu.RLock()
	defer s.mu.RUnlock()
	location := query.Location
	if location == nil {
		location = time.Local
	}
	counts := map[time.Time]int{}
	for _, e := range s.events {
		if matchesQuery(e, query.Index, query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime) {
			counts[memoryTimelineBucketStart(e.EventTime, query.Interval, location)]++
		}
	}
	buckets := make([]TimelineBucket, 0, len(counts))
	for start, count := range counts {
		buckets = append(buckets, TimelineBucket{Start: start, Count: count})
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Start.Before(buckets[j].Start)
	})
	return buckets
}

func memoryTimelineBucketStart(value time.Time, interval string, location *time.Location) time.Time {
	local := value.In(location)
	year, month, day := local.Date()
	switch interval {
	case "minute":
		return time.Date(year, month, day, local.Hour(), local.Minute(), 0, 0, location)
	case "day":
		return time.Date(year, month, day, 0, 0, 0, 0, location)
	case "month":
		return time.Date(year, month, 1, 0, 0, 0, 0, location)
	default:
		return time.Date(year, month, day, local.Hour(), 0, 0, 0, location)
	}
}

func (s *Store) Indexes() []IndexInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byIndex := map[string]*IndexInfo{}
	for _, e := range s.events {
		index := stringify(e.Metadata["index"])
		if index == "" {
			index = "app"
		}
		item := byIndex[index]
		if item == nil {
			item = &IndexInfo{IndexName: index, TTLDays: 30}
			byIndex[index] = item
		}
		item.Rows++
		item.StorageBytes += len(e.Raw)
		if e.EventTime.After(item.LatestEventTime) {
			item.LatestEventTime = e.EventTime
		}
	}
	items := make([]IndexInfo, 0, len(byIndex))
	for _, item := range byIndex {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].IndexName < items[j].IndexName })
	return items
}

func (s *Store) Stats(query StatsQuery) splstats.Result {
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	fields := make([]string, 0, len(query.Stats.GroupBy)+len(query.Stats.Aggregates))
	for _, group := range query.Stats.GroupBy {
		fields = append(fields, group.DisplayName())
	}
	for _, agg := range query.Stats.Aggregates {
		fields = append(fields, agg.DisplayName())
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	groups := map[string]*memoryStatsGroup{}
	order := []string{}
	for _, e := range s.events {
		if !matchesQuery(e, query.Index, query.Keyword, query.Field, query.Value, query.StartTime, query.EndTime) {
			continue
		}
		keyParts := make([]string, 0, len(query.Stats.GroupBy))
		groupValues := map[string]any{}
		for _, group := range query.Stats.GroupBy {
			value, _ := fieldValue(e, group)
			text := stringify(value)
			keyParts = append(keyParts, text)
			groupValues[group.DisplayName()] = text
		}
		key := strings.Join(keyParts, "\x00")
		item := groups[key]
		if item == nil {
			item = &memoryStatsGroup{values: groupValues, aggs: make([]memoryAggState, len(query.Stats.Aggregates))}
			groups[key] = item
			order = append(order, key)
		}
		for i, agg := range query.Stats.Aggregates {
			item.aggs[i].add(e, agg)
		}
	}

	rows := make([]map[string]any, 0, len(groups))
	for _, key := range order {
		group := groups[key]
		row := map[string]any{}
		for _, field := range query.Stats.GroupBy {
			row[field.DisplayName()] = group.values[field.DisplayName()]
		}
		for i, agg := range query.Stats.Aggregates {
			row[agg.DisplayName()] = group.aggs[i].value(agg)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := numericAny(rows[i][query.Stats.Aggregates[0].DisplayName()])
		right, _ := numericAny(rows[j][query.Stats.Aggregates[0].DisplayName()])
		return left > right
	})
	if offset >= len(rows) {
		rows = []map[string]any{}
	} else {
		end := offset + limit
		if end > len(rows) {
			end = len(rows)
		}
		rows = rows[offset:end]
	}
	return splstats.Result{Query: query.Stats.Raw, Fields: fields, Rows: rows, Limit: limit}
}

func matchesQuery(e *event.Event, index string, keyword string, field string, value string, startTime time.Time, endTime time.Time) bool {
	if !startTime.IsZero() && e.EventTime.Before(startTime) {
		return false
	}
	if !endTime.IsZero() && e.EventTime.After(endTime) {
		return false
	}
	if index != "" && e.Metadata["index"] != index {
		return false
	}
	if keyword != "" && !strings.Contains(strings.ToLower(e.Raw), strings.ToLower(keyword)) {
		return false
	}
	if field != "" {
		v, ok := fieldValue(e, splstats.FieldRef{Name: field})
		if !ok || stringify(v) != value {
			return false
		}
	}
	return true
}

type memoryStatsGroup struct {
	values map[string]any
	aggs   []memoryAggState
}

type memoryAggState struct {
	count int
	sum   float64
	min   float64
	max   float64
	seen  bool
}

func (s *memoryAggState) add(e *event.Event, agg splstats.Aggregate) {
	if agg.Func == "count" && agg.Field == nil {
		s.count++
		return
	}
	if agg.Field == nil {
		return
	}
	value, ok := fieldValue(e, *agg.Field)
	if agg.Func == "count" {
		if ok && stringify(value) != "" {
			s.count++
		}
		return
	}
	number, ok := numericAny(value)
	if !ok {
		return
	}
	s.count++
	s.sum += number
	if !s.seen || number < s.min {
		s.min = number
	}
	if !s.seen || number > s.max {
		s.max = number
	}
	s.seen = true
}

func (s memoryAggState) value(agg splstats.Aggregate) any {
	switch agg.Func {
	case "count":
		return s.count
	case "sum":
		return s.sum
	case "avg":
		if s.count == 0 {
			return nil
		}
		return s.sum / float64(s.count)
	case "min":
		if !s.seen {
			return nil
		}
		return s.min
	case "max":
		if !s.seen {
			return nil
		}
		return s.max
	default:
		return nil
	}
}

func fieldValue(e *event.Event, field splstats.FieldRef) (any, bool) {
	switch field.Scope {
	case "", "fields":
		if value, ok := metadataFieldValue(e, field.Name); ok {
			return value, true
		}
		value, ok := e.Fields[field.Name]
		return value, ok
	case "metadata":
		if field.Name == "index" {
			value, ok := e.Metadata["index"]
			return value, ok
		}
		value, ok := e.Metadata[field.Name]
		return value, ok
	case "source":
		switch field.Name {
		case "type":
			return e.Source.Type, true
		case "name":
			return e.Source.Name, true
		case "host":
			return e.Source.Host, true
		case "ip":
			return e.Source.IP, true
		}
	case "root":
		switch field.Name {
		case "index":
			value, ok := e.Metadata["index"]
			return value, ok
		case "raw_length":
			return len(e.Raw), true
		}
	}
	return nil, false
}

func metadataFieldValue(e *event.Event, name string) (any, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "source":
		return e.Source.Name, true
	case "index":
		value, ok := e.Metadata["index"]
		return value, ok
	case "sourcetype", "vendor", "product", "parse_status", "parse_rule_id", "parse_rule_name", "parse_error", "parsed_at":
		value, ok := e.Metadata[name]
		return value, ok
	default:
		return nil, false
	}
}

func numericAny(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(v, 64)
		return n, err == nil
	default:
		n, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		return n, err == nil
	}
}

func stringify(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	b, _ := json.Marshal(value)
	return string(b)
}
