package mvp

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"xdp/pkg/pipeline"
	ch "xdp/pkg/storage/clickhouse"
	mysqlstore "xdp/pkg/storage/mysql"
)

type ParserPlugin struct {
	PluginCode          string         `json:"plugin_code"`
	PluginType          string         `json:"plugin_type"`
	DisplayName         string         `json:"display_name"`
	Category            string         `json:"category"`
	Description         string         `json:"description,omitempty"`
	Version             string         `json:"version"`
	Runtime             string         `json:"runtime,omitempty"`
	Phase               string         `json:"phase,omitempty"`
	ConfigSchema        map[string]any `json:"config_schema,omitempty"`
	SchemaSummary       map[string]any `json:"schema_summary,omitempty"`
	ValidationRules     map[string]any `json:"validation_rules,omitempty"`
	PropsTemplate       string         `json:"props_template,omitempty"`
	RuntimeCapabilities map[string]any `json:"runtime_capabilities"`
	Status              string         `json:"status"`
	Builtin             bool           `json:"builtin"`
	ConfiguredCount     int            `json:"configured_count"`
}

type ParseRule struct {
	ID                  string         `json:"id"`
	Code                string         `json:"code,omitempty"`
	Name                string         `json:"name"`
	Status              string         `json:"status"`
	ParserPlugin        string         `json:"parser_plugin"`
	ParserPluginVersion string         `json:"parser_plugin_version,omitempty"`
	DataSourceID        string         `json:"data_source_id,omitempty"`
	DataSourceName      string         `json:"data_source_name,omitempty"`
	InputRoute          string         `json:"input_route"`
	OutputIndex         string         `json:"output_index"`
	Source              string         `json:"source,omitempty"`
	Sourcetype          string         `json:"sourcetype,omitempty"`
	Priority            int            `json:"priority"`
	Stage               string         `json:"stage"`
	SampleEvent         string         `json:"sample_event,omitempty"`
	PluginConfig        map[string]any `json:"plugin_config"`
	PropsConf           string         `json:"props_conf"`
	PreviewResult       []PreviewField `json:"preview_result,omitempty"`
	HotFields           []ch.HotField  `json:"hot_fields,omitempty"`
	ValidationResult    map[string]any `json:"validation_result,omitempty"`
	PipelineID          string         `json:"pipeline_id,omitempty"`
	LastPublishedAt     *time.Time     `json:"last_published_at,omitempty"`
	LastError           string         `json:"last_error,omitempty"`
	CreatedAt           time.Time      `json:"created_at,omitempty"`
	UpdatedAt           time.Time      `json:"updated_at,omitempty"`
}

type PreviewField struct {
	Field string `json:"field"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

func defaultParseRules() map[string]ParseRule {
	return map[string]ParseRule{}
}

func (h *Handler) listParserPlugins(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	h.mu.RLock()
	for _, rule := range h.parseRules {
		if rule.Status == "deleted" {
			continue
		}
		counts[rule.ParserPlugin]++
	}
	h.mu.RUnlock()
	plugins := parserPluginCatalog(counts)
	writeJSON(w, http.StatusOK, map[string]any{"plugins": plugins})
}

func (h *Handler) listParseRules(w http.ResponseWriter, r *http.Request) {
	if h.mysql != nil {
		if items, err := h.mysql.ListParseRules(r.Context()); err == nil {
			rules := make([]ParseRule, 0, len(items))
			for _, item := range items {
				rule, err := parseRuleFromStore(item)
				if err == nil {
					rules = append(rules, rule)
				}
			}
			rules = filterParseRules(rules, r.URL.Query())
			sortParseRules(rules)
			pageItems, pagination := paginateList(rules, r)
			writeJSON(w, http.StatusOK, map[string]any{"parse_rules": pageItems, "pagination": pagination})
			return
		}
	}
	h.mu.RLock()
	items := make([]ParseRule, 0, len(h.parseRules))
	for _, rule := range h.parseRules {
		if rule.Status != "deleted" {
			items = append(items, rule)
		}
	}
	h.mu.RUnlock()
	items = filterParseRules(items, r.URL.Query())
	sortParseRules(items)
	pageItems, pagination := paginateList(items, r)
	writeJSON(w, http.StatusOK, map[string]any{"parse_rules": pageItems, "pagination": pagination})
}

func sortParseRules(items []ParseRule) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		left := strings.ToLower(firstNonEmpty(items[i].Name, items[i].Code, items[i].ID))
		right := strings.ToLower(firstNonEmpty(items[j].Name, items[j].Code, items[j].ID))
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
}

func (h *Handler) getParseRule(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	rule, ok := h.findParseRule(id)
	if !ok || rule.Status == "deleted" {
		writeErrorCode(w, http.StatusNotFound, "PARSE_RULE_NOT_FOUND", "parse rule not found")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *Handler) createParseRule(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	rule, apiErr := decodeParseRule(r, true)
	if apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	normalized, apiErr := h.normalizeParseRule(rule, "", true)
	if apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	if err := h.persistParseRule(r, normalized); err != nil {
		writeError(w, http.StatusBadGateway, "save parse rule failed")
		return
	}
	writeJSON(w, http.StatusOK, normalized)
}

func (h *Handler) updateParseRule(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	id := strings.TrimSpace(r.PathValue("id"))
	if _, ok := h.findParseRule(id); !ok {
		writeErrorCode(w, http.StatusNotFound, "PARSE_RULE_NOT_FOUND", "parse rule not found")
		return
	}
	rule, apiErr := decodeParseRule(r, true)
	if apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	normalized, apiErr := h.normalizeParseRule(rule, id, true)
	if apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	if err := h.persistParseRule(r, normalized); err != nil {
		writeError(w, http.StatusBadGateway, "save parse rule failed")
		return
	}
	writeJSON(w, http.StatusOK, normalized)
}

func (h *Handler) updateParseRuleStatus(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	id := strings.TrimSpace(r.PathValue("id"))
	rule, ok := h.findParseRule(id)
	if !ok || rule.Status == "deleted" {
		writeErrorCode(w, http.StatusNotFound, "PARSE_RULE_NOT_FOUND", "parse rule not found")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid status request")
		return
	}
	status := normalizeStatus(req.Status)
	if status != "active" && status != "disabled" {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", "status must be active or disabled")
		return
	}
	rule.Status = status
	rule.UpdatedAt = time.Now().UTC()
	if err := h.persistParseRule(r, rule); err != nil {
		writeError(w, http.StatusBadGateway, "save parse rule failed")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *Handler) deleteParseRule(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	rule, ok := h.findParseRule(id)
	if !ok || rule.Status == "deleted" {
		writeErrorCode(w, http.StatusNotFound, "PARSE_RULE_NOT_FOUND", "parse rule not found")
		return
	}
	rule.Status = "deleted"
	rule.UpdatedAt = time.Now().UTC()
	if h.mysql != nil {
		if err := h.mysql.DeleteParseRule(r.Context(), id); err != nil {
			writeError(w, http.StatusBadGateway, "delete parse rule failed")
			return
		}
	}
	h.mu.Lock()
	delete(h.parseRules, id)
	h.runtimePipelines = h.buildRuntimePipelinesLocked()
	h.mu.Unlock()
	h.publishRuntimePipelines(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (h *Handler) testParseRule(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	rule, apiErr := decodeParseRule(r, false)
	if apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	normalized, apiErr := h.normalizeParseRule(rule, strings.TrimSpace(r.PathValue("id")), false)
	if apiErr != nil {
		writeErrorCode(w, apiErr.status, apiErr.code, apiErr.message)
		return
	}
	fields, err := previewParseRule(normalized)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "fields": fields, "errors": []any{}})
}

func decodeParseRule(r *http.Request, requireName bool) (ParseRule, *collectAPIError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return ParseRule{}, &collectAPIError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "invalid parse rule json"}
	}
	if _, ok := raw["time_field"]; ok {
		return ParseRule{}, validationError("time_field is not accepted; use props_conf time settings")
	}
	data, _ := json.Marshal(raw)
	var rule ParseRule
	if err := json.Unmarshal(data, &rule); err != nil {
		return ParseRule{}, &collectAPIError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "invalid parse rule fields"}
	}
	if requireName && strings.TrimSpace(rule.Name) == "" {
		return ParseRule{}, validationError("name is required")
	}
	return rule, nil
}

func (h *Handler) normalizeParseRule(rule ParseRule, pathID string, requireName bool) (ParseRule, *collectAPIError) {
	now := time.Now().UTC()
	rule.Name = strings.TrimSpace(rule.Name)
	if requireName && rule.Name == "" {
		return ParseRule{}, validationError("name is required")
	}
	rule.ParserPlugin = strings.ToLower(strings.TrimSpace(rule.ParserPlugin))
	if rule.ParserPlugin == "" {
		return ParseRule{}, validationError("parser_plugin is required")
	}
	if _, ok := supportedParserPlugins()[rule.ParserPlugin]; !ok {
		return ParseRule{}, &collectAPIError{status: http.StatusUnprocessableEntity, code: "PARSER_PLUGIN_NOT_SUPPORTED", message: "parser_plugin is not supported"}
	}
	rule.Status = normalizeStatus(rule.Status)
	if rule.Status != "active" && rule.Status != "disabled" && rule.Status != "deleted" {
		return ParseRule{}, validationError("status must be active or disabled")
	}
	rule.Stage = strings.ToLower(strings.TrimSpace(rule.Stage))
	if rule.Stage == "" {
		rule.Stage = "ingest"
	}
	if rule.Stage != "ingest" {
		return ParseRule{}, validationError("stage must be ingest")
	}
	if rule.ParserPlugin == "regex" {
		rule.ParserPluginVersion = "1.0.0"
	} else {
		item, ok := h.findPlugin("parser", rule.ParserPlugin, "")
		if !ok {
			return ParseRule{}, &collectAPIError{status: http.StatusUnprocessableEntity, code: "PARSER_PLUGIN_NOT_SUPPORTED", message: "parser plugin is not installed"}
		}
		if item.Status != "enabled" && item.Status != "active" {
			return ParseRule{}, &collectAPIError{status: http.StatusUnprocessableEntity, code: "PARSER_PLUGIN_NOT_ENABLED", message: "parser plugin is not enabled"}
		}
		rule.ParserPluginVersion = item.PluginVersion
	}
	rule.InputRoute = firstNonEmpty(strings.TrimSpace(rule.InputRoute), "internal_raw_topic")
	if strings.TrimSpace(rule.OutputIndex) == "" {
		return ParseRule{}, validationError("output_index is required")
	}
	outputIndex, err := ch.NormalizeIndexName(rule.OutputIndex)
	if err != nil {
		return ParseRule{}, validationError("output_index is invalid")
	}
	if ch.IsSystemIndexName(outputIndex) {
		return ParseRule{}, validationError("output_index cannot be a system index")
	}
	rule.OutputIndex = outputIndex
	rule.DataSourceID = strings.TrimSpace(rule.DataSourceID)
	rule.DataSourceName = strings.TrimSpace(rule.DataSourceName)
	rule.Source = strings.TrimSpace(rule.Source)
	rule.Sourcetype = strings.TrimSpace(rule.Sourcetype)
	if rule.Priority == 0 {
		rule.Priority = 100
	}
	if rule.PluginConfig == nil {
		rule.PluginConfig = map[string]any{}
	}
	if rule.ParserPlugin == "regex" {
		rule.PluginConfig = normalizeRegexPluginConfig(rule.PluginConfig)
	}
	hotFields, err := ch.NormalizeHotFields(rule.HotFields)
	if err != nil {
		return ParseRule{}, validationError("hot_fields is invalid")
	}
	rule.HotFields = hotFields
	if err := validateParseRule(rule); err != nil {
		return ParseRule{}, validationError(err.Error())
	}
	if strings.TrimSpace(rule.SampleEvent) != "" {
		preview, err := previewParseRule(rule)
		if err == nil {
			rule.PreviewResult = preview
			if len(rule.HotFields) == 0 {
				rule.HotFields = deriveInternalHotFieldsFromPreview(preview)
			}
		}
	}
	id := strings.TrimSpace(pathID)
	if id == "" || id == "preview" {
		id = strings.TrimSpace(rule.ID)
	}
	if id == "" || id == "preview" {
		id = h.uniqueParseRuleID("pr_"+parseRuleCode(firstNonEmpty(rule.Code, rule.Name, rule.ParserPlugin)), false)
	}
	rule.ID = id
	if rule.Code == "" {
		rule.Code = parseRuleCode(firstNonEmpty(rule.Name, strings.TrimPrefix(id, "pr_")))
	}
	if rule.Code == "" {
		rule.Code = strings.TrimPrefix(id, "pr_")
	}
	rule.PipelineID = firstNonEmpty(rule.PipelineID, "pipe_parse_"+strings.ReplaceAll(rule.Code, "-", "_"))
	rule.ValidationResult = map[string]any{"valid": true}
	rule.LastError = ""
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	if rule.Status == "active" {
		rule.LastPublishedAt = &now
	}
	return rule, nil
}

func validateParseRule(rule ParseRule) error {
	if strings.TrimSpace(rule.DataSourceName) == "" {
		return fmt.Errorf("data_source_name is required")
	}
	if strings.Contains(rule.PropsConf, "XDP_INPUT_ROUTE") {
		return fmt.Errorf("input_route must not be written into props_conf")
	}
	if err := validatePropsConf(rule.PropsConf); err != nil {
		return err
	}
	switch rule.ParserPlugin {
	case "json-parser":
		if stringConfig(rule.PluginConfig, "source_field", "raw") != "raw" {
			return fmt.Errorf("plugin_config.source_field must be raw")
		}
		if stringConfig(rule.PluginConfig, "target", "fields") != "fields" {
			return fmt.Errorf("plugin_config.target must be fields")
		}
		if strings.TrimSpace(stringConfig(rule.PluginConfig, "flatten_separator", ".")) == "" {
			return fmt.Errorf("plugin_config.flatten_separator is required")
		}
		arrayMode := stringConfig(rule.PluginConfig, "array_mode", "json_string")
		if arrayMode != "json_string" && arrayMode != "expand_index" {
			return fmt.Errorf("plugin_config.array_mode must be json_string or expand_index")
		}
		onInvalidJSON := stringConfig(rule.PluginConfig, "on_invalid_json", "continue")
		if onInvalidJSON != "continue" && onInvalidJSON != "fail" {
			return fmt.Errorf("plugin_config.on_invalid_json must be continue or fail")
		}
		if sample := strings.TrimSpace(rule.SampleEvent); sample != "" {
			var value any
			decoder := json.NewDecoder(strings.NewReader(sample))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				return fmt.Errorf("sample_event is not valid json")
			}
		}
	case "regex":
		if strings.TrimSpace(rule.SampleEvent) == "" {
			return fmt.Errorf("sample_event is required")
		}
		if stringConfig(rule.PluginConfig, "source_field", "raw") != "raw" {
			return fmt.Errorf("plugin_config.source_field must be raw")
		}
		if stringConfig(rule.PluginConfig, "target", "fields") != "fields" {
			return fmt.Errorf("plugin_config.target must be fields")
		}
		if stringConfig(rule.PluginConfig, "on_no_match", "continue") != "continue" {
			return fmt.Errorf("plugin_config.on_no_match must be continue")
		}
		pattern := stringConfig(rule.PluginConfig, "regex_pattern", "")
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("plugin_config.regex_pattern is required")
		}
		re, err := regexp.Compile(goRegexPattern(pattern))
		if err != nil {
			return fmt.Errorf("plugin_config.regex_pattern is invalid")
		}
		if !regexHasNamedCapture(re) {
			return fmt.Errorf("plugin_config.regex_pattern must include named capture groups")
		}
	case "kv":
		if strings.TrimSpace(stringConfig(rule.PluginConfig, "field_delimiter", "")) == "" {
			return fmt.Errorf("plugin_config.field_delimiter is required")
		}
		if strings.TrimSpace(stringConfig(rule.PluginConfig, "kv_delimiter", "")) == "" {
			return fmt.Errorf("plugin_config.kv_delimiter is required")
		}
	case "delimited":
		if strings.TrimSpace(stringConfig(rule.PluginConfig, "field_delimiter", "")) == "" {
			return fmt.Errorf("plugin_config.field_delimiter is required")
		}
		if len(fieldNamesConfig(rule.PluginConfig["field_names"])) == 0 {
			return fmt.Errorf("plugin_config.field_names is required")
		}
	}
	return nil
}

func validatePropsConf(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("props_conf is required")
	}
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			continue
		}
		if strings.Contains(trimmed, "=") {
			continue
		}
		return fmt.Errorf("props_conf line %d is invalid", i+1)
	}
	return nil
}

func previewParseRule(rule ParseRule) ([]PreviewField, error) {
	sample := rule.SampleEvent
	switch rule.ParserPlugin {
	case "json-parser":
		var value any
		decoder := json.NewDecoder(strings.NewReader(sample))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("sample_event is not valid json")
		}
		return flattenPreviewJSON(value, ""), nil
	case "regex":
		pattern := stringConfig(rule.PluginConfig, "regex_pattern", "")
		re, err := regexp.Compile(goRegexPattern(pattern))
		if err != nil {
			return nil, fmt.Errorf("plugin_config.regex_pattern is invalid")
		}
		match := re.FindStringSubmatch(sample)
		if match == nil {
			return []PreviewField{}, nil
		}
		names := re.SubexpNames()
		fields := make([]PreviewField, 0, len(match)-1)
		for i := 1; i < len(match); i++ {
			name := ""
			if i < len(names) {
				name = names[i]
			}
			if name == "" {
				continue
			}
			fields = append(fields, PreviewField{Field: name, Value: match[i], Type: previewValueType(match[i])})
		}
		return fields, nil
	case "kv":
		return previewKVFields(sample, rule.PluginConfig), nil
	case "delimited":
		return previewDelimitedFields(sample, rule.PluginConfig)
	default:
		return nil, fmt.Errorf("parser_plugin is not supported")
	}
}

func flattenPreviewJSON(value any, prefix string) []PreviewField {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fields := []PreviewField{}
		for _, key := range keys {
			name := key
			if prefix != "" {
				name = prefix + "." + key
			}
			fields = append(fields, flattenPreviewJSON(typed[key], name)...)
		}
		return fields
	case []any:
		valueText, _ := json.Marshal(typed)
		return []PreviewField{{Field: firstNonEmpty(prefix, "root"), Value: string(valueText), Type: "array"}}
	case json.Number:
		return []PreviewField{{Field: firstNonEmpty(prefix, "root"), Value: typed.String(), Type: "number"}}
	case bool:
		return []PreviewField{{Field: firstNonEmpty(prefix, "root"), Value: fmt.Sprintf("%t", typed), Type: "boolean"}}
	case nil:
		return []PreviewField{{Field: firstNonEmpty(prefix, "root"), Value: "null", Type: "null"}}
	default:
		return []PreviewField{{Field: firstNonEmpty(prefix, "root"), Value: fmt.Sprint(typed), Type: previewValueType(fmt.Sprint(typed))}}
	}
}

func previewKVFields(sample string, config map[string]any) []PreviewField {
	kvDelimiter := stringConfig(config, "kv_delimiter", "=")
	pattern := `([\w.@-]+)\s*` + regexp.QuoteMeta(kvDelimiter) + `\s*("[^"]*"|'[^']*'|\S+)`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(sample, -1)
	fields := make([]PreviewField, 0, len(matches))
	for _, match := range matches {
		value := stripPreviewQuotes(match[2])
		fields = append(fields, PreviewField{Field: match[1], Value: value, Type: previewValueType(value)})
	}
	return fields
}

func previewDelimitedFields(sample string, config map[string]any) ([]PreviewField, error) {
	delimiter := resolveConfigDelimiter(stringConfig(config, "field_delimiter", ","))
	fields := fieldNamesConfig(config["field_names"])
	reader := csv.NewReader(strings.NewReader(sample))
	reader.Comma = []rune(delimiter)[0]
	reader.FieldsPerRecord = -1
	record, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("sample_event is not valid delimited text")
	}
	out := make([]PreviewField, 0, len(record))
	for i, value := range record {
		name := fmt.Sprintf("field_%d", i+1)
		if i < len(fields) {
			name = fields[i]
		}
		out = append(out, PreviewField{Field: name, Value: stripPreviewQuotes(value), Type: previewValueType(value)})
	}
	return out, nil
}

func deriveInternalHotFieldsFromPreview(fields []PreviewField) []ch.HotField {
	out := make([]ch.HotField, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Field)
		if name == "" || field.Type == "error" || field.Type == "null" || field.Type == "array" {
			continue
		}
		fieldType := internalHotFieldType(field)
		normalized, err := ch.NormalizeHotFields([]ch.HotField{{
			Name:         name,
			Type:         fieldType,
			Searchable:   internalHotFieldSearchable(fieldType),
			Aggregatable: true,
		}})
		if err != nil || len(normalized) == 0 {
			continue
		}
		out = append(out, normalized[0])
	}
	return out
}

func internalHotFieldType(field PreviewField) string {
	name := strings.ToLower(strings.TrimSpace(field.Field))
	value := strings.TrimSpace(field.Value)
	if field.Type == "number" || regexp.MustCompile(`^\d+$`).MatchString(value) {
		return "uint64"
	}
	if field.Type == "boolean" {
		return "bool"
	}
	if name == "action" || name == "status" || name == "method" || strings.HasSuffix(name, "_type") {
		return "low_cardinality_string"
	}
	return "string"
}

func internalHotFieldSearchable(fieldType string) bool {
	return fieldType == "string" || fieldType == "low_cardinality_string" || fieldType == "bool"
}

func normalizeRegexPluginConfig(config map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range config {
		out[key] = value
	}
	out["source_field"] = stringConfig(out, "source_field", "raw")
	out["target"] = stringConfig(out, "target", "fields")
	out["on_no_match"] = stringConfig(out, "on_no_match", "continue")
	if _, ok := out["field_types"]; !ok {
		out["field_types"] = map[string]any{}
	}
	return out
}

func regexHasNamedCapture(re *regexp.Regexp) bool {
	for i, name := range re.SubexpNames() {
		if i > 0 && strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

func (h *Handler) findParseRule(id string) (ParseRule, bool) {
	id = strings.TrimSpace(id)
	if h.mysql != nil {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		if item, err := h.mysql.GetParseRule(ctx, id); err == nil {
			rule, err := parseRuleFromStore(item)
			if err == nil {
				return rule, true
			}
		}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	rule, ok := h.parseRules[id]
	return rule, ok
}

func (h *Handler) persistParseRule(r *http.Request, rule ParseRule) error {
	if os.Getenv("XDP_OUTPUT") == "clickhouse" && rule.Status == "active" && len(rule.HotFields) > 0 {
		if err := h.clickhouse.EnsureHotFields(r.Context(), rule.OutputIndex, rule.HotFields); err != nil {
			return err
		}
	}
	if h.mysql != nil {
		if err := h.mysql.SaveParseRule(r.Context(), parseRuleToStore(rule)); err != nil {
			return err
		}
	}
	h.mu.Lock()
	h.parseRules[rule.ID] = rule
	h.runtimePipelines = h.buildRuntimePipelinesLocked()
	h.mu.Unlock()
	h.publishRuntimePipelines(r.Context())
	return nil
}

func filterParseRules(items []ParseRule, values url.Values) []ParseRule {
	parserPlugin := strings.ToLower(strings.TrimSpace(values.Get("parser_plugin")))
	status := strings.ToLower(strings.TrimSpace(values.Get("status")))
	dataSourceName := strings.TrimSpace(values.Get("data_source_name"))
	inputRoute := strings.TrimSpace(values.Get("input_route"))
	outputIndex := strings.TrimSpace(values.Get("output_index"))
	sourcetype := strings.TrimSpace(values.Get("sourcetype"))
	out := make([]ParseRule, 0, len(items))
	for _, item := range items {
		if item.Status == "deleted" {
			continue
		}
		if parserPlugin != "" && item.ParserPlugin != parserPlugin {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		if dataSourceName != "" && item.DataSourceName != dataSourceName {
			continue
		}
		if inputRoute != "" && item.InputRoute != inputRoute {
			continue
		}
		if outputIndex != "" && item.OutputIndex != outputIndex {
			continue
		}
		if sourcetype != "" && item.Sourcetype != sourcetype {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func parserPluginCatalog(counts map[string]int) []ParserPlugin {
	return []ParserPlugin{
		regexParserPlugin(counts["regex"]),
	}
}

func regexParserPlugin(count int) ParserPlugin {
	return ParserPlugin{
		PluginCode:  "regex",
		PluginType:  "parser",
		DisplayName: "正则解析插件",
		Category:    "text",
		Description: "P0 内置解析插件，通过手动正则表达式和命名捕获抽取字段。",
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Phase:       "P0",
		ConfigSchema: map[string]any{
			"type":     "object",
			"required": []string{"regex_pattern"},
			"properties": map[string]any{
				"source_field":  map[string]any{"type": "string", "default": "raw", "enum": []string{"raw"}},
				"regex_pattern": map[string]any{"type": "string"},
				"target":        map[string]any{"type": "string", "default": "fields", "enum": []string{"fields"}},
				"field_types":   map[string]any{"type": "object"},
				"on_no_match":   map[string]any{"type": "string", "default": "continue", "enum": []string{"continue"}},
			},
		},
		SchemaSummary: map[string]any{
			"required": []string{"sample_event", "regex_pattern", "props_conf"},
			"defaults": map[string]any{"source_field": "raw", "target": "fields", "on_no_match": "continue"},
		},
		ValidationRules: map[string]any{"named_capture_required": true},
		PropsTemplate:   "[source::<rule_name>]\nEXTRACT-custom = <regex_pattern>",
		RuntimeCapabilities: map[string]any{
			"preview":             true,
			"props_conf_generate": true,
			"runtime_publish":     true,
			"runtime_ingest":      true,
			"ingest_parse":        true,
			"hot_reload":          true,
		},
		Status:          "active",
		Builtin:         true,
		ConfiguredCount: count,
	}
}

func p1ParserPlugin(code string, name string, category string, description string, count int, schema map[string]any) ParserPlugin {
	return ParserPlugin{
		PluginCode:      code,
		PluginType:      "parser",
		DisplayName:     name,
		Category:        category,
		Description:     description,
		Version:         "1.0.0",
		Runtime:         "go_builtin",
		Phase:           "P1",
		ConfigSchema:    schema,
		SchemaSummary:   map[string]any{"phase": "P1", "enabled": false},
		ValidationRules: map[string]any{},
		RuntimeCapabilities: map[string]any{
			"preview":             false,
			"props_conf_generate": false,
			"runtime_publish":     false,
			"runtime_ingest":      false,
			"ingest_parse":        false,
			"hot_reload":          false,
		},
		Status:          "disabled",
		Builtin:         true,
		ConfiguredCount: count,
	}
}

func supportedParserPlugins() map[string]struct{} {
	return map[string]struct{}{"regex": {}, "json-parser": {}}
}

func parserPluginStoreItems() []mysqlstore.ParserPlugin {
	plugins := parserPluginCatalog(map[string]int{})
	items := make([]mysqlstore.ParserPlugin, 0, len(plugins))
	for _, plugin := range plugins {
		schema, _ := json.Marshal(plugin.ConfigSchema)
		rules, _ := json.Marshal(plugin.ValidationRules)
		capabilities, _ := json.Marshal(plugin.RuntimeCapabilities)
		items = append(items, mysqlstore.ParserPlugin{
			PluginCode:          plugin.PluginCode,
			PluginType:          plugin.PluginType,
			DisplayName:         plugin.DisplayName,
			Category:            plugin.Category,
			Description:         plugin.Description,
			Version:             plugin.Version,
			ConfigSchema:        schema,
			ValidationRules:     rules,
			PropsTemplate:       plugin.PropsTemplate,
			RuntimeCapabilities: capabilities,
			Status:              plugin.Status,
			Builtin:             plugin.Builtin,
		})
	}
	return items
}

func parseRuleToStore(rule ParseRule) mysqlstore.ParseRule {
	config, _ := json.Marshal(rule.PluginConfig)
	preview, _ := json.Marshal(rule.PreviewResult)
	validation, _ := json.Marshal(rule.ValidationResult)
	hotFields, _ := json.Marshal(rule.HotFields)
	return mysqlstore.ParseRule{
		ID:                  rule.ID,
		Code:                rule.Code,
		Name:                rule.Name,
		Status:              rule.Status,
		ParserPlugin:        rule.ParserPlugin,
		ParserPluginVersion: rule.ParserPluginVersion,
		DataSourceID:        rule.DataSourceID,
		DataSourceName:      rule.DataSourceName,
		InputRoute:          rule.InputRoute,
		OutputIndex:         rule.OutputIndex,
		Source:              rule.Source,
		Sourcetype:          rule.Sourcetype,
		Priority:            rule.Priority,
		Stage:               rule.Stage,
		SampleEvent:         rule.SampleEvent,
		PluginConfig:        config,
		PropsConf:           rule.PropsConf,
		PreviewResult:       preview,
		ValidationResult:    validation,
		HotFields:           hotFields,
		PipelineID:          rule.PipelineID,
		LastPublishedAt:     rule.LastPublishedAt,
		LastError:           rule.LastError,
		CreatedAt:           rule.CreatedAt,
		UpdatedAt:           rule.UpdatedAt,
	}
}

func parseRuleFromStore(item mysqlstore.ParseRule) (ParseRule, error) {
	var config map[string]any
	if len(item.PluginConfig) > 0 {
		if err := json.Unmarshal(item.PluginConfig, &config); err != nil {
			return ParseRule{}, err
		}
	}
	var preview []PreviewField
	if len(item.PreviewResult) > 0 {
		_ = json.Unmarshal(item.PreviewResult, &preview)
	}
	var validation map[string]any
	if len(item.ValidationResult) > 0 {
		_ = json.Unmarshal(item.ValidationResult, &validation)
	}
	var hotFields []ch.HotField
	if len(item.HotFields) > 0 {
		_ = json.Unmarshal(item.HotFields, &hotFields)
	}
	return ParseRule{
		ID:                  item.ID,
		Code:                item.Code,
		Name:                item.Name,
		Status:              item.Status,
		ParserPlugin:        item.ParserPlugin,
		ParserPluginVersion: item.ParserPluginVersion,
		DataSourceID:        item.DataSourceID,
		DataSourceName:      item.DataSourceName,
		InputRoute:          item.InputRoute,
		OutputIndex:         firstNonEmpty(item.OutputIndex, "app"),
		Source:              item.Source,
		Sourcetype:          item.Sourcetype,
		Priority:            item.Priority,
		Stage:               item.Stage,
		SampleEvent:         item.SampleEvent,
		PluginConfig:        config,
		PropsConf:           item.PropsConf,
		PreviewResult:       preview,
		ValidationResult:    validation,
		HotFields:           hotFields,
		PipelineID:          item.PipelineID,
		LastPublishedAt:     item.LastPublishedAt,
		LastError:           item.LastError,
		CreatedAt:           item.CreatedAt,
		UpdatedAt:           item.UpdatedAt,
	}, nil
}

func (h *Handler) uniqueParseRuleID(base string, updating bool) string {
	base = strings.Trim(base, "-_")
	if base == "" {
		base = "pr_rule"
	}
	if len(base) > 36 {
		base = base[:36]
	}
	if updating {
		return base
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.parseRules[base]; !ok {
		return base
	}
	for i := 2; i < 1000; i++ {
		suffix := fmt.Sprintf("_%d", i)
		candidate := base
		if len(candidate)+len(suffix) > 36 {
			candidate = candidate[:36-len(suffix)]
		}
		candidate += suffix
		if _, ok := h.parseRules[candidate]; !ok {
			return candidate
		}
	}
	return fmt.Sprintf("pr_%d", time.Now().UnixNano())
}

func parseRuleCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('_')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func propsConfForParseRule(rule ParseRule) string {
	sourceName := parseRuleCode(firstNonEmpty(rule.Source, rule.Sourcetype, rule.Name, rule.ParserPlugin))
	if sourceName == "" {
		sourceName = "custom"
	}
	switch rule.ParserPlugin {
	case "json":
		return fmt.Sprintf("[source::%s]\nINDEXED_EXTRACTIONS = json\nKV_MODE = none", sourceName)
	case "regex":
		pattern := stringConfig(rule.PluginConfig, "regex_pattern", "field=(?<field>\\S+)")
		return fmt.Sprintf("[source::%s]\nEXTRACT-custom = %s", sourceName, pattern)
	case "kv":
		fieldDelimiter := stringConfig(rule.PluginConfig, "field_delimiter", " ")
		fieldQuote := stringConfig(rule.PluginConfig, "field_quote", "\"")
		return fmt.Sprintf("[source::%s]\nKV_MODE = auto\nFIELD_DELIMITER = %s\nFIELD_QUOTE = %s", sourceName, fieldDelimiter, fieldQuote)
	case "delimited":
		fieldDelimiter := stringConfig(rule.PluginConfig, "field_delimiter", ",")
		fieldNames := strings.Join(fieldNamesConfig(rule.PluginConfig["field_names"]), ",")
		fieldQuote := stringConfig(rule.PluginConfig, "field_quote", "\"")
		return fmt.Sprintf("[source::%s]\nINDEXED_EXTRACTIONS = csv\nFIELD_DELIMITER = %s\nFIELD_NAMES = %s\nFIELD_QUOTE = %s", sourceName, fieldDelimiter, fieldNames, fieldQuote)
	default:
		return fmt.Sprintf("[source::%s]", sourceName)
	}
}

func (h *Handler) parseRulesForSourceLocked(source DataSource) []ParseRule {
	rules := make([]ParseRule, 0, len(h.parseRules))
	for _, rule := range h.parseRules {
		if parseRuleMatchesSource(rule, source) {
			rules = append(rules, rule)
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority == rules[j].Priority {
			return rules[i].Code < rules[j].Code
		}
		return rules[i].Priority < rules[j].Priority
	})
	return rules
}

func (h *Handler) appendParseRuleStagesLocked(stages []pipeline.StageSpec, source DataSource) []pipeline.StageSpec {
	rules := h.parseRulesForSourceLocked(source)
	if len(rules) == 0 {
		return append([]pipeline.StageSpec(nil), stages...)
	}
	group := pipeline.StageSpec{
		ID:     "parse-rule-group",
		Type:   "parser_group",
		Config: map[string]any{"fallback_output_index": ch.SystemUnparsedIndexName},
		Stages: make([]pipeline.StageSpec, 0, len(rules)),
	}
	for _, rule := range rules {
		group.Stages = append(group.Stages, h.parseRuleStageLocked(rule))
	}
	out := []pipeline.StageSpec{group}
	for _, stage := range stages {
		if stage.Type == "parser" || stage.Type == "router" {
			continue
		}
		out = append(out, stage)
	}
	return out
}

func (h *Handler) parseRuleStage(rule ParseRule) pipeline.StageSpec {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.parseRuleStageLocked(rule)
}

func (h *Handler) parseRuleStageLocked(rule ParseRule) pipeline.StageSpec {
	if rule.ParserPlugin != "regex" {
		if item, ok := h.importedPlugins[pluginKey("parser", rule.ParserPlugin, "")]; ok {
			rule.ParserPluginVersion = item.PluginVersion
		}
	}
	return parseRuleStage(rule)
}

func (h *Handler) parseRuleOutputIndexLocked(source DataSource) string {
	for _, rule := range h.parseRulesForSourceLocked(source) {
		if strings.TrimSpace(rule.OutputIndex) != "" {
			return rule.OutputIndex
		}
	}
	return firstNonEmpty(source.DefaultIndex, "app")
}

func (h *Handler) parseRuleOutputIndex(source DataSource) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.parseRuleOutputIndexLocked(source)
}

func applyPipelineOutputIndex(pipe *pipeline.Pipeline, index string) {
	index = firstNonEmpty(index, "app")
	for _, stage := range pipe.Spec.Stages {
		if stage.Type == "parser_group" {
			index = "${metadata.index}"
			break
		}
	}
	for i := range pipe.Spec.Outputs {
		if pipe.Spec.Outputs[i].Config == nil {
			pipe.Spec.Outputs[i].Config = map[string]any{}
		}
		pipe.Spec.Outputs[i].Config["index"] = index
	}
	for i := range pipe.Spec.Stages {
		rules, ok := pipe.Spec.Stages[i].Config["rules"].([]any)
		if !ok {
			continue
		}
		for _, rawRule := range rules {
			ruleMap, ok := rawRule.(map[string]any)
			if !ok {
				continue
			}
			setMap, ok := ruleMap["set"].(map[string]any)
			if !ok {
				continue
			}
			if _, ok := setMap["metadata.index"]; ok {
				setMap["metadata.index"] = index
			}
		}
	}
}

func parseRuleMatchesSource(rule ParseRule, source DataSource) bool {
	if rule.Status != "active" || rule.Stage != "ingest" {
		return false
	}
	if rule.DataSourceID != "" {
		return rule.DataSourceID == source.ID
	}
	if rule.DataSourceName != "" {
		return rule.DataSourceName == source.Name
	}
	if rule.InputRoute == "" || rule.InputRoute == "internal_raw_topic" {
		return true
	}
	if rule.InputRoute == source.InternalRawTopic {
		return true
	}
	return false
}

func (h *Handler) hotFieldsForIndex(index string) []ch.HotField {
	index = firstNonEmpty(strings.TrimSpace(index), "app")
	if normalized, err := ch.NormalizeIndexName(index); err == nil {
		index = normalized
	}
	h.mu.RLock()
	rules := make([]ParseRule, 0, len(h.parseRules))
	for _, rule := range h.parseRules {
		rules = append(rules, rule)
	}
	h.mu.RUnlock()
	byName := map[string]ch.HotField{}
	for _, rule := range rules {
		if rule.Status != "active" || rule.OutputIndex != index {
			continue
		}
		for _, field := range rule.HotFields {
			if field.Name == "" {
				continue
			}
			byName[field.Name] = field
		}
	}
	fields := make([]ch.HotField, 0, len(byName))
	for _, field := range byName {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func parseRuleStage(rule ParseRule) pipeline.StageSpec {
	hotFields := make([]any, 0, len(rule.HotFields))
	for _, field := range rule.HotFields {
		hotFields = append(hotFields, map[string]any{"name": field.Name, "type": field.Type, "searchable": field.Searchable, "aggregatable": field.Aggregatable, "aliases": field.Aliases})
	}
	pluginCode := "props-conf-parser"
	runtimeVersion := rule.ParserPluginVersion
	config := map[string]any{
		"parser_plugin":          rule.ParserPlugin,
		"plugin_package_version": rule.ParserPluginVersion,
		"props_conf":             rule.PropsConf,
		"input_route":            rule.InputRoute,
		"output_index":           rule.OutputIndex,
		"data_source_name":       rule.DataSourceName,
		"sourcetype":             rule.Name,
		"rule_id":                rule.ID,
		"plugin_config":          rule.PluginConfig,
		"hot_fields":             hotFields,
	}
	if rule.ParserPlugin == "regex" || rule.ParserPlugin == "json-parser" {
		pluginCode = rule.ParserPlugin
		for key, value := range rule.PluginConfig {
			config[key] = value
		}
	}
	if rule.ParserPlugin == "json-parser" {
		runtimeVersion = "1.0.0"
	}
	return pipeline.StageSpec{
		ID:      "parse-rule-" + rule.Code,
		Type:    "parser",
		Plugin:  pluginCode,
		Version: runtimeVersion,
		Config:  config,
		OnError: &pipeline.ErrorPolicy{Action: "continue"},
	}
}

func goRegexPattern(pattern string) string {
	return strings.ReplaceAll(pattern, "(?<", "(?P<")
}

func fieldNamesConfig(value any) []string {
	switch typed := value.(type) {
	case []string:
		return cleanFieldNames(typed)
	case []any:
		names := make([]string, 0, len(typed))
		for _, item := range typed {
			names = append(names, fmt.Sprint(item))
		}
		return cleanFieldNames(names)
	case string:
		return cleanFieldNames(strings.Split(typed, ","))
	default:
		return nil
	}
}

func cleanFieldNames(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveConfigDelimiter(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "", "逗号":
		return ","
	case "空格":
		return " "
	case "制表符", "\\t":
		return "\t"
	case "竖杠":
		return "|"
	case "分号":
		return ";"
	default:
		return value
	}
}

func stripPreviewQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func previewValueType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "string"
	}
	if value == "true" || value == "false" || value == "TRUE" || value == "FALSE" {
		return "boolean"
	}
	if regexp.MustCompile(`^-?\d+(\.\d+)?$`).MatchString(value) {
		return "number"
	}
	return "string"
}
