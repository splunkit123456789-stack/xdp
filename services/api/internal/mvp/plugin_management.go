package mvp

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"xdp/pkg/plugin"
	mysqlstore "xdp/pkg/storage/mysql"
)

type pluginManifest struct {
	PluginCode         string         `json:"plugin_code"`
	PluginType         string         `json:"plugin_type"`
	PluginVersion      string         `json:"plugin_version"`
	Code               string         `json:"code"`
	Type               string         `json:"type"`
	Version            string         `json:"version"`
	Name               string         `json:"name"`
	DisplayName        string         `json:"display_name"`
	Description        string         `json:"description"`
	Runtime            string         `json:"runtime"`
	Entrypoint         string         `json:"entrypoint"`
	MinPlatformVersion string         `json:"min_platform_version"`
	ConfigSchema       map[string]any `json:"config_schema"`
	UISchema           map[string]any `json:"ui_schema"`
	InputSchema        map[string]any `json:"input_schema"`
	OutputSchema       map[string]any `json:"output_schema"`
	PermissionSchema   map[string]any `json:"permission_schema"`
	RuntimeConfig      map[string]any `json:"runtime_config"`
	Capabilities       map[string]any `json:"capabilities"`
}

const currentPlatformVersion = "0.3.0"

var pluginCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

type pluginPackageError struct {
	code    string
	message string
}

func (e pluginPackageError) Error() string { return e.message }

func newPluginPackageError(code, message string) pluginPackageError {
	return pluginPackageError{code: code, message: message}
}

type PluginImportResponse struct {
	PluginCode       string         `json:"plugin_code"`
	PluginType       string         `json:"plugin_type"`
	PluginVersion    string         `json:"plugin_version"`
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Runtime          string         `json:"runtime"`
	Entrypoint       string         `json:"entrypoint,omitempty"`
	Status           string         `json:"status"`
	Checksum         string         `json:"checksum"`
	ConfigSchema     map[string]any `json:"config_schema,omitempty"`
	UISchema         map[string]any `json:"ui_schema,omitempty"`
	InputSchema      map[string]any `json:"input_schema,omitempty"`
	OutputSchema     map[string]any `json:"output_schema,omitempty"`
	PermissionSchema map[string]any `json:"permission_schema,omitempty"`
	RuntimeConfig    map[string]any `json:"runtime_config,omitempty"`
	PackageBytes     []byte         `json:"-"`
}

func readPluginPackage(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, fmt.Errorf("invalid multipart plugin package")
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("plugin file is required")
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("read plugin file failed")
		}
		return data, nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, newPluginPackageError("PLUGIN_PACKAGE_INVALID", "read plugin package failed")
	}
	if len(data) == 0 {
		return nil, newPluginPackageError("PLUGIN_PACKAGE_EMPTY", "plugin package is empty")
	}
	return data, nil
}

func parsePluginManifest(data []byte) (pluginManifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return pluginManifest{}, newPluginPackageError("PLUGIN_PACKAGE_INVALID", "plugin package must be a zip file")
	}
	for _, file := range zr.File {
		if file.Name != "manifest.json" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return pluginManifest{}, fmt.Errorf("open manifest failed")
		}
		defer rc.Close()
		var manifest pluginManifest
		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			return pluginManifest{}, newPluginPackageError("PLUGIN_MANIFEST_INVALID", "invalid manifest json")
		}
		if err := manifest.validate(); err != nil {
			return pluginManifest{}, err
		}
		return manifest, nil
	}
	return pluginManifest{}, newPluginPackageError("PLUGIN_MANIFEST_MISSING", "manifest.json is required")
}

func validatePluginPackageAssets(data []byte, manifest pluginManifest) error {
	if strings.TrimSpace(manifest.runtime()) != "executable_search_command" {
		return nil
	}
	if normalizePluginType(manifest.pluginType()) != "search_command" {
		return newPluginPackageError("PLUGIN_RUNTIME_UNSUPPORTED", "executable_search_command only supports search_command plugins")
	}
	entrypoint := cleanPluginEntrypoint(manifest.Entrypoint)
	if entrypoint == "" {
		return newPluginPackageError("PLUGIN_ENTRYPOINT_INVALID", "executable_search_command requires entrypoint")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return newPluginPackageError("PLUGIN_PACKAGE_INVALID", "plugin package must be a zip file")
	}
	for _, file := range zr.File {
		if cleanZipPath(file.Name) == entrypoint && !file.FileInfo().IsDir() {
			return nil
		}
	}
	return newPluginPackageError("PLUGIN_ENTRYPOINT_MISSING", "entrypoint file is missing from plugin package")
}

func (m pluginManifest) validate() error {
	code := m.code()
	pluginType := normalizePluginType(m.pluginType())
	if strings.TrimSpace(code) == "" {
		return newPluginPackageError("PLUGIN_MANIFEST_INVALID", "plugin_code is required")
	}
	if !pluginCodePattern.MatchString(code) {
		return newPluginPackageError("PLUGIN_CODE_INVALID", "plugin_code format is invalid")
	}
	if pluginType == "" {
		return newPluginPackageError("PLUGIN_MANIFEST_INVALID", "plugin_type is required")
	}
	if m.version() == "" {
		return newPluginPackageError("PLUGIN_MANIFEST_INVALID", "plugin_version is required")
	}
	if !supportedImportedPluginType(pluginType) {
		return newPluginPackageError("PLUGIN_TYPE_UNSUPPORTED", "unsupported plugin_type")
	}
	if productVisibleBuiltinPlugin(pluginType, code) {
		return newPluginPackageError("BUILTIN_PLUGIN_PROTECTED", "builtin plugin code is protected")
	}
	if !supportedPluginRuntime(m.runtime()) {
		return newPluginPackageError("PLUGIN_RUNTIME_UNSUPPORTED", "plugin runtime is unsupported")
	}
	if !platformVersionCompatible(m.MinPlatformVersion) {
		return newPluginPackageError("PLUGIN_PLATFORM_INCOMPATIBLE", "plugin requires a newer platform version")
	}
	if err := validatePluginSchemas(m.ConfigSchema, m.UISchema); err != nil {
		return err
	}
	return nil
}

func (m pluginManifest) code() string {
	if strings.TrimSpace(m.PluginCode) != "" {
		return strings.TrimSpace(m.PluginCode)
	}
	return strings.TrimSpace(m.Code)
}

func (m pluginManifest) pluginType() string {
	if strings.TrimSpace(m.PluginType) != "" {
		return strings.TrimSpace(m.PluginType)
	}
	return strings.TrimSpace(m.Type)
}

func (m pluginManifest) version() string {
	if strings.TrimSpace(m.PluginVersion) != "" {
		return strings.TrimSpace(m.PluginVersion)
	}
	return strings.TrimSpace(m.Version)
}

func (m pluginManifest) runtime() string {
	runtime := strings.TrimSpace(m.Runtime)
	if runtime == "" {
		return "go_builtin"
	}
	return runtime
}

func (m pluginManifest) displayName() string {
	if strings.TrimSpace(m.Name) != "" {
		return strings.TrimSpace(m.Name)
	}
	if strings.TrimSpace(m.DisplayName) != "" {
		return strings.TrimSpace(m.DisplayName)
	}
	return m.code()
}

func (m pluginManifest) toImportResponse(checksum string) PluginImportResponse {
	pluginType := normalizePluginType(m.pluginType())
	version := m.version()
	return PluginImportResponse{
		PluginCode:       m.code(),
		PluginType:       pluginType,
		PluginVersion:    version,
		Name:             m.displayName(),
		Description:      strings.TrimSpace(m.Description),
		Runtime:          m.runtime(),
		Entrypoint:       strings.TrimSpace(m.Entrypoint),
		Status:           "disabled",
		Checksum:         checksum,
		ConfigSchema:     ensureSchemaMap(m.ConfigSchema),
		UISchema:         ensureSchemaMap(m.UISchema),
		InputSchema:      ensureSchemaMap(m.InputSchema),
		OutputSchema:     ensureSchemaMap(m.OutputSchema),
		PermissionSchema: ensureSchemaMap(m.PermissionSchema),
		RuntimeConfig:    ensureSchemaMap(m.RuntimeConfig),
	}
}

func (p PluginImportResponse) withStatus(status string) PluginImportResponse {
	p.Status = status
	return p
}

func (p PluginImportResponse) key() string {
	return pluginKey(p.PluginType, p.PluginCode, "")
}

func (p PluginImportResponse) toStoreRecord() mysqlstore.PluginRecord {
	return mysqlstore.PluginRecord{
		PluginCode:       p.PluginCode,
		PluginType:       p.PluginType,
		PluginVersion:    p.PluginVersion,
		Name:             p.Name,
		Description:      p.Description,
		Runtime:          p.Runtime,
		Entrypoint:       p.Entrypoint,
		Status:           p.Status,
		Checksum:         p.Checksum,
		ConfigSchema:     mustJSONRaw(p.ConfigSchema),
		UISchema:         mustJSONRaw(p.UISchema),
		InputSchema:      mustJSONRaw(p.InputSchema),
		OutputSchema:     mustJSONRaw(p.OutputSchema),
		PermissionSchema: mustJSONRaw(p.PermissionSchema),
		RuntimeConfig:    mustJSONRaw(p.RuntimeConfig),
		PackageBytes:     p.PackageBytes,
	}
}

func pluginChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizePluginType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "input", "collector", "collect":
		return "input"
	case "parser", "parse":
		return "parser"
	case "search_command", "search-command", "search":
		return "search_command"
	case "spl_function", "spl-function", "function":
		return "spl_function"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func supportedImportedPluginType(value string) bool {
	switch value {
	case "input", "parser", "search_command":
		return true
	default:
		return false
	}
}

func supportedPluginRuntime(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "go_builtin", "go_plugin", "external", "declarative_search_command", "executable_search_command":
		return true
	default:
		return false
	}
}

func cleanPluginEntrypoint(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "./")
	value = cleanZipPath(value)
	if value == "." || value == "/" {
		return ""
	}
	return value
}

func cleanZipPath(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "/")
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func platformVersionCompatible(minVersion string) bool {
	minVersion = strings.TrimSpace(minVersion)
	if minVersion == "" {
		return true
	}
	return compareSemanticVersion(currentPlatformVersion, minVersion) >= 0
}

func compareSemanticVersion(left, right string) int {
	leftParts := semanticVersionParts(left)
	rightParts := semanticVersionParts(right)
	for i := 0; i < len(leftParts) || i < len(rightParts); i++ {
		var l, r int
		if i < len(leftParts) {
			l = leftParts[i]
		}
		if i < len(rightParts) {
			r = rightParts[i]
		}
		if l > r {
			return 1
		}
		if l < r {
			return -1
		}
	}
	return 0
}

func semanticVersionParts(value string) []int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return []int{0}
	}
	segments := strings.Split(value, ".")
	parts := make([]int, 0, len(segments))
	for _, segment := range segments {
		clean := segment
		if idx := strings.IndexAny(clean, "-+"); idx >= 0 {
			clean = clean[:idx]
		}
		n, err := strconv.Atoi(clean)
		if err != nil {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}

func validatePluginSchemas(configSchema map[string]any, uiSchema map[string]any) error {
	if configSchema == nil {
		configSchema = map[string]any{}
	}
	if uiSchema == nil {
		uiSchema = map[string]any{}
	}
	if len(configSchema) > 0 {
		if typ, ok := configSchema["type"].(string); ok && typ != "" && typ != "object" {
			return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "config_schema must describe an object")
		}
		if typ, ok := configSchema["type"].(string); !ok || typ == "" {
			return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "config_schema must declare type object")
		}
	}
	properties := map[string]any{}
	if rawProperties, ok := configSchema["properties"].(map[string]any); ok {
		properties = rawProperties
	}
	if rawRequired, ok := configSchema["required"]; ok {
		items, ok := rawRequired.([]any)
		if !ok {
			return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "config_schema.required must be an array")
		}
		for _, item := range items {
			name, ok := item.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "config_schema.required contains invalid field")
			}
			if _, exists := properties[name]; !exists {
				return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "config_schema.required references unknown field")
			}
		}
	}
	for name, rawProperty := range properties {
		property, ok := rawProperty.(map[string]any)
		if !ok {
			continue
		}
		if sensitiveFieldName(name) && !propertyMarksSensitive(property) {
			return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "sensitive config field must be marked sensitive")
		}
	}
	if rawOrder, ok := uiSchema["order"]; ok {
		items, ok := rawOrder.([]any)
		if !ok {
			return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema.order must be an array")
		}
		for _, item := range items {
			name, ok := item.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema.order contains invalid field")
			}
			if len(properties) > 0 {
				if _, exists := properties[name]; !exists {
					return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema references unknown config field")
				}
			}
		}
	}
	if rawGroups, ok := uiSchema["groups"]; ok {
		groups, ok := rawGroups.([]any)
		if !ok {
			return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema.groups must be an array")
		}
		for _, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema.groups contains invalid group")
			}
			rawFields, exists := group["fields"]
			if !exists {
				continue
			}
			fields, ok := rawFields.([]any)
			if !ok {
				return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema.groups.fields must be an array")
			}
			for _, field := range fields {
				name, ok := field.(string)
				if !ok || strings.TrimSpace(name) == "" {
					return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema.groups.fields contains invalid field")
				}
				if len(properties) > 0 {
					if _, exists := properties[name]; !exists {
						return newPluginPackageError("PLUGIN_SCHEMA_INVALID", "ui_schema references unknown config field")
					}
				}
			}
		}
	}
	return nil
}

func sensitiveFieldName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(name, "password") ||
		strings.Contains(name, "token") ||
		strings.Contains(name, "secret") ||
		strings.Contains(name, "private_key")
}

func propertyMarksSensitive(property map[string]any) bool {
	for _, key := range []string{"x-sensitive", "sensitive"} {
		if value, ok := property[key].(bool); ok && value {
			return true
		}
	}
	return false
}

func pluginResponsesFromMetadata(items []plugin.Metadata, filterType string) []PluginImportResponse {
	out := make([]PluginImportResponse, 0, len(items))
	for _, meta := range items {
		pluginType := normalizePluginType(string(meta.Type))
		if filterType != "" && pluginType != filterType {
			continue
		}
		if !productVisibleBuiltinPlugin(pluginType, meta.Code) {
			continue
		}
		status := "active"
		if strings.EqualFold(meta.Labels["status"], "disabled") {
			status = "disabled"
		}
		out = append(out, normalizePluginResponse(PluginImportResponse{
			PluginCode:       meta.Code,
			PluginType:       pluginType,
			PluginVersion:    meta.Version,
			Name:             firstNonEmpty(meta.Name, meta.Code),
			Description:      meta.Description,
			Runtime:          meta.Runtime,
			Status:           status,
			Checksum:         firstNonEmpty(meta.Labels["checksum"], "builtin"),
			ConfigSchema:     ensureSchemaMap(meta.ConfigSchema),
			UISchema:         ensureSchemaMap(meta.UISchema),
			InputSchema:      ensureSchemaMap(meta.InputSchema),
			OutputSchema:     ensureSchemaMap(meta.OutputSchema),
			PermissionSchema: ensureSchemaMap(meta.PermissionSchema),
			RuntimeConfig:    map[string]any{},
		}))
	}
	return deduplicatePluginResponses(out)
}

func pluginResponsesFromRecords(items []mysqlstore.PluginRecord, filterType string) []PluginImportResponse {
	out := make([]PluginImportResponse, 0, len(items))
	for _, item := range items {
		pluginType := normalizePluginType(item.PluginType)
		if filterType != "" && pluginType != filterType {
			continue
		}
		if !productVisiblePluginRecord(pluginType, item) {
			continue
		}
		out = append(out, normalizePluginResponse(PluginImportResponse{
			PluginCode:       item.PluginCode,
			PluginType:       pluginType,
			PluginVersion:    item.PluginVersion,
			Name:             firstNonEmpty(item.Name, item.PluginCode),
			Description:      item.Description,
			Runtime:          item.Runtime,
			Entrypoint:       item.Entrypoint,
			Status:           firstNonEmpty(item.Status, "disabled"),
			Checksum:         item.Checksum,
			ConfigSchema:     rawToSchemaMap(item.ConfigSchema),
			UISchema:         rawToSchemaMap(item.UISchema),
			InputSchema:      rawToSchemaMap(item.InputSchema),
			OutputSchema:     rawToSchemaMap(item.OutputSchema),
			PermissionSchema: rawToSchemaMap(item.PermissionSchema),
			RuntimeConfig:    rawToSchemaMap(item.RuntimeConfig),
			PackageBytes:     item.PackageBytes,
		}))
	}
	return deduplicatePluginResponses(out)
}

func normalizePluginResponse(item PluginImportResponse) PluginImportResponse {
	item.PluginCode = strings.TrimSpace(item.PluginCode)
	item.PluginType = normalizePluginType(item.PluginType)
	item.PluginVersion = strings.TrimSpace(item.PluginVersion)
	if item.PluginVersion == "" {
		item.PluginVersion = "1.0.0"
	}
	if item.Name == "" {
		item.Name = item.PluginCode
	}
	if item.Runtime == "" {
		item.Runtime = "go_builtin"
	}
	item.RuntimeConfig = ensureSchemaMap(item.RuntimeConfig)
	if productVisibleBuiltinPlugin(item.PluginType, item.PluginCode) {
		item.Status = "enabled"
		if strings.TrimSpace(item.Checksum) == "" {
			item.Checksum = "builtin"
		}
	}
	return item
}

func deduplicatePluginResponses(items []PluginImportResponse) []PluginImportResponse {
	current := map[string]PluginImportResponse{}
	for _, item := range items {
		item = normalizePluginResponse(item)
		key := pluginKey(item.PluginType, item.PluginCode, "")
		if existing, ok := current[key]; !ok || preferCurrentPlugin(item, existing) {
			current[key] = item
		}
	}
	out := make([]PluginImportResponse, 0, len(current))
	for _, item := range current {
		out = append(out, item)
	}
	return out
}

func productVisiblePluginRecord(pluginType string, item mysqlstore.PluginRecord) bool {
	if pluginType == "spl_function" {
		return false
	}
	if hiddenProductPluginCode(pluginType, item.PluginCode) {
		return false
	}
	checksum := strings.TrimSpace(item.Checksum)
	if checksum != "" && checksum != "builtin" {
		return supportedImportedPluginType(pluginType)
	}
	return productVisibleBuiltinPlugin(pluginType, item.PluginCode)
}

func productVisibleBuiltinPlugin(pluginType, code string) bool {
	switch pluginType {
	case "input":
		return code == "syslog"
	case "parser":
		return code == "regex"
	case "search_command":
		return code == "stats"
	default:
		return false
	}
}

func hiddenProductPluginCode(pluginType, code string) bool {
	switch pluginType + "/" + strings.TrimSpace(code) {
	case "input/http-input", "parser/props-conf-parser":
		return true
	default:
		return false
	}
}

func ensureSchemaMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func rawToSchemaMap(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func mustJSONRaw(value map[string]any) json.RawMessage {
	data, err := json.Marshal(ensureSchemaMap(value))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}
