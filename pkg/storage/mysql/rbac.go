package mysql

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	ch "xdp/pkg/storage/clickhouse"
)

type IndexScopeMap map[string][]string

type EffectiveIndexScope struct {
	Restricted bool     `json:"restricted"`
	Patterns   []string `json:"patterns,omitempty"`
}

type PluginScopeBinding struct {
	PluginType string `json:"plugin_type"`
	PluginCode string `json:"plugin_code"`
}

type PluginScopeMap map[string][]PluginScopeBinding

type AuthRoleRecord struct {
	ID              string         `json:"id"`
	RoleCode        string         `json:"role_code"`
	RoleName        string         `json:"role_name"`
	Description     string         `json:"description,omitempty"`
	Status          string         `json:"status"`
	Builtin         bool           `json:"builtin"`
	PermissionCodes []string       `json:"permission_codes,omitempty"`
	IndexScopes     IndexScopeMap  `json:"index_scopes,omitempty"`
	PluginScopes    PluginScopeMap `json:"plugin_scopes,omitempty"`
	UserCount       int            `json:"user_count,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
}

type AuthPermissionRecord struct {
	ID             string    `json:"id"`
	PermissionCode string    `json:"permission_code"`
	ResourceCode   string    `json:"resource_code"`
	ActionCode     string    `json:"action_code"`
	DisplayName    string    `json:"display_name"`
	Description    string    `json:"description,omitempty"`
	Status         string    `json:"status"`
	Builtin        bool      `json:"builtin"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

type AuthUserRecord struct {
	ID               string           `json:"id"`
	Username         string           `json:"username"`
	DisplayName      string           `json:"display_name,omitempty"`
	Status           string           `json:"status"`
	RoleLabel        string           `json:"role_label,omitempty"`
	RoleCodes        []string         `json:"role_codes,omitempty"`
	Roles            []AuthRoleRecord `json:"roles,omitempty"`
	LastLoginAt      *time.Time       `json:"last_login_at,omitempty"`
	FailedLoginCount int              `json:"failed_login_count,omitempty"`
	CreatedAt        time.Time        `json:"created_at,omitempty"`
	UpdatedAt        time.Time        `json:"updated_at,omitempty"`
}

type AuthTokenRecord struct {
	ID              string         `json:"id"`
	UserID          string         `json:"user_id"`
	Username        string         `json:"username,omitempty"`
	TokenName       string         `json:"token_name"`
	TokenPrefix     string         `json:"token_prefix,omitempty"`
	TokenType       string         `json:"token_type"`
	Source          string         `json:"source"`
	Status          string         `json:"status"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
	LastUsedAt      *time.Time     `json:"last_used_at,omitempty"`
	RevokedAt       *time.Time     `json:"revoked_at,omitempty"`
	PermissionCodes []string       `json:"permission_codes,omitempty"`
	IndexScopes     IndexScopeMap  `json:"index_scopes,omitempty"`
	PluginScopes    PluginScopeMap `json:"plugin_scopes,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
}

type AuthAuditLogRecord struct {
	ID        string          `json:"id"`
	UserID    string          `json:"user_id,omitempty"`
	Username  string          `json:"username,omitempty"`
	EventType string          `json:"event_type"`
	Result    string          `json:"result"`
	RequestID string          `json:"request_id,omitempty"`
	SourceIP  string          `json:"source_ip,omitempty"`
	UserAgent string          `json:"user_agent,omitempty"`
	Method    string          `json:"method,omitempty"`
	Path      string          `json:"path,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
}

type RBACListPage struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type UserFilter struct {
	Page     int
	PageSize int
	Query    string
	Status   string
}

type CreateUserRequest struct {
	Username    string
	DisplayName string
	Password    string
	Status      string
	RoleIDs     []string
	ActorID     string
}

type UpdateUserRequest struct {
	ID          string
	DisplayName string
	Status      string
	ActorID     string
}

type CreateRoleRequest struct {
	RoleCode        string
	RoleName        string
	Description     string
	Status          string
	PermissionCodes []string
	IndexScopes     IndexScopeMap
	PluginScopes    PluginScopeMap
	ActorID         string
}

type UpdateRoleRequest struct {
	ID              string
	RoleName        string
	Description     string
	Status          string
	PermissionCodes []string
	IndexScopes     IndexScopeMap
	PluginScopes    PluginScopeMap
	ActorID         string
}

type CreateScopedTokenRequest struct {
	UserID          string
	TokenName       string
	ActorID         string
	ExpiresAt       *time.Time
	PermissionCodes []string
	IndexScopes     IndexScopeMap
	PluginScopes    PluginScopeMap
}

type CreatedTokenRecord struct {
	TokenID               string                         `json:"token_id"`
	UserID                string                         `json:"user_id"`
	TokenName             string                         `json:"token_name"`
	TokenPrefix           string                         `json:"token_prefix"`
	Plaintext             string                         `json:"plaintext"`
	ExpiresAt             *time.Time                     `json:"expires_at,omitempty"`
	EffectiveScopes       []string                       `json:"effective_scopes,omitempty"`
	EffectivePermissions  []string                       `json:"effective_permissions,omitempty"`
	EffectiveIndexScopes  map[string]EffectiveIndexScope `json:"effective_index_scopes,omitempty"`
	EffectivePluginScopes PluginScopeMap                 `json:"effective_plugin_scopes,omitempty"`
}

type ResolvedPrincipalRecord struct {
	UserID                string
	Username              string
	DisplayName           string
	UserStatus            string
	TokenID               string
	TokenName             string
	TokenStatus           string
	TokenSource           string
	TokenExpiresAt        *time.Time
	Roles                 []AuthRoleRecord
	UserPermissionCodes   []string
	TokenPermissionCodes  []string
	EffectivePermissions  []string
	UserIndexScopes       IndexScopeMap
	TokenIndexScopes      IndexScopeMap
	EffectiveIndexScopes  map[string]EffectiveIndexScope
	UserPluginScopes      PluginScopeMap
	TokenPluginScopes     PluginScopeMap
	EffectivePluginScopes PluginScopeMap
}

type BuiltinPermission struct {
	Code        string
	Resource    string
	Action      string
	DisplayName string
	Description string
}

type BuiltinRole struct {
	Code            string
	Name            string
	Description     string
	PermissionCodes []string
	PluginScopes    PluginScopeMap
}

var indexScopePatternRegexp = regexp.MustCompile(`^[a-z0-9_]+(\*)?$`)
var pluginScopeCodeRegexp = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var supportedIndexScopeActions = []string{"manage", "read", "search"}
var supportedPluginScopeActions = []string{"manage", "use"}
var supportedPluginScopeTypes = []string{"input", "parser", "search_command"}

var builtinRBACPermissions = []BuiltinPermission{
	{Code: "user:read", Resource: "user", Action: "read", DisplayName: "查看用户", Description: "查看用户列表与详情"},
	{Code: "user:create", Resource: "user", Action: "create", DisplayName: "创建用户", Description: "新增用户"},
	{Code: "user:update", Resource: "user", Action: "update", DisplayName: "修改用户", Description: "修改用户信息"},
	{Code: "user:disable", Resource: "user", Action: "disable", DisplayName: "禁用用户", Description: "禁用或启用用户"},
	{Code: "user:delete", Resource: "user", Action: "delete", DisplayName: "删除用户", Description: "删除用户"},
	{Code: "user:reset_password", Resource: "user", Action: "reset_password", DisplayName: "重置密码", Description: "重置用户密码"},
	{Code: "role:read", Resource: "role", Action: "read", DisplayName: "查看角色", Description: "查看角色和权限"},
	{Code: "role:create", Resource: "role", Action: "create", DisplayName: "创建角色", Description: "新增角色"},
	{Code: "role:update", Resource: "role", Action: "update", DisplayName: "修改角色", Description: "修改角色与权限"},
	{Code: "role:delete", Resource: "role", Action: "delete", DisplayName: "删除角色", Description: "删除角色"},
	{Code: "token:read", Resource: "token", Action: "read", DisplayName: "查看 Token", Description: "查看 Token 列表"},
	{Code: "token:create", Resource: "token", Action: "create", DisplayName: "创建 Token", Description: "创建受限 Token"},
	{Code: "token:revoke", Resource: "token", Action: "revoke", DisplayName: "吊销 Token", Description: "吊销 Token"},
	{Code: "rbac:manage", Resource: "rbac", Action: "manage", DisplayName: "用户与权限管理", Description: "管理用户、角色、权限和 scope"},
	{Code: "datasource:read", Resource: "datasource", Action: "read", DisplayName: "查看采集源", Description: "查看采集源与运行状态"},
	{Code: "datasource:create", Resource: "datasource", Action: "create", DisplayName: "新增采集源", Description: "创建采集源"},
	{Code: "datasource:update", Resource: "datasource", Action: "update", DisplayName: "修改采集源", Description: "编辑采集源"},
	{Code: "datasource:delete", Resource: "datasource", Action: "delete", DisplayName: "删除采集源", Description: "删除采集源"},
	{Code: "datasource:start", Resource: "datasource", Action: "start", DisplayName: "启动采集源", Description: "启动采集监听"},
	{Code: "datasource:stop", Resource: "datasource", Action: "stop", DisplayName: "停止采集源", Description: "停止采集监听"},
	{Code: "parse_rule:read", Resource: "parse_rule", Action: "read", DisplayName: "查看解析规则", Description: "查看解析规则"},
	{Code: "parse_rule:create", Resource: "parse_rule", Action: "create", DisplayName: "新增解析规则", Description: "创建解析规则"},
	{Code: "parse_rule:update", Resource: "parse_rule", Action: "update", DisplayName: "修改解析规则", Description: "修改解析规则"},
	{Code: "parse_rule:delete", Resource: "parse_rule", Action: "delete", DisplayName: "删除解析规则", Description: "删除解析规则"},
	{Code: "parse_rule:test", Resource: "parse_rule", Action: "test", DisplayName: "测试解析规则", Description: "预览解析结果"},
	{Code: "index:read", Resource: "index", Action: "read", DisplayName: "查看索引", Description: "查看索引配置"},
	{Code: "index:manage", Resource: "index", Action: "manage", DisplayName: "管理索引", Description: "创建、修改和删除索引"},
	{Code: "index:create", Resource: "index", Action: "create", DisplayName: "新增索引", Description: "创建索引"},
	{Code: "index:update", Resource: "index", Action: "update", DisplayName: "修改索引", Description: "修改索引配置"},
	{Code: "index:delete", Resource: "index", Action: "delete", DisplayName: "删除索引", Description: "删除索引"},
	{Code: "index:trend", Resource: "index", Action: "trend", DisplayName: "查看趋势", Description: "查看索引容量趋势"},
	{Code: "search:execute", Resource: "search", Action: "execute", DisplayName: "执行搜索", Description: "执行 SPL 搜索"},
	{Code: "search:fields", Resource: "search", Action: "fields", DisplayName: "查看字段", Description: "查看搜索字段摘要"},
	{Code: "search:timeline", Resource: "search", Action: "timeline", DisplayName: "查看时间线", Description: "查看搜索时间线"},
	{Code: "search:saved_search", Resource: "search", Action: "saved_search", DisplayName: "保存搜索", Description: "管理保存搜索"},
	{Code: "audit:read", Resource: "audit", Action: "read", DisplayName: "查看审计", Description: "查看安全审计日志"},
}

var builtinRBACRoles = []BuiltinRole{
	{
		Code:            "platform_admin",
		Name:            "平台管理员",
		Description:     "拥有全部权限",
		PermissionCodes: allBuiltinPermissionCodes(),
		PluginScopes:    allBuiltinPluginScopes(),
	},
	{
		Code:        "config_admin",
		Name:        "配置管理员",
		Description: "管理采集、解析、索引和插件",
		PermissionCodes: []string{
			"datasource:read", "datasource:create", "datasource:update", "datasource:delete", "datasource:start", "datasource:stop",
			"parse_rule:read", "parse_rule:create", "parse_rule:update", "parse_rule:delete", "parse_rule:test",
			"index:read", "index:manage", "index:create", "index:update", "index:delete", "index:trend",
			"search:execute", "search:fields", "search:timeline", "search:saved_search",
		},
		PluginScopes: allBuiltinPluginScopes(),
	},
	{
		Code:        "analyst",
		Name:        "分析师",
		Description: "执行搜索并查看授权配置",
		PermissionCodes: []string{
			"datasource:read",
			"parse_rule:read",
			"index:read", "index:trend",
			"search:execute", "search:fields", "search:timeline", "search:saved_search",
		},
		PluginScopes: PluginScopeMap{
			"use": {
				{PluginType: "search_command", PluginCode: "stats"},
			},
		},
	},
	{
		Code:        "readonly",
		Name:        "只读用户",
		Description: "只读查看配置和搜索",
		PermissionCodes: []string{
			"datasource:read",
			"parse_rule:read",
			"index:read", "index:trend",
			"search:execute", "search:fields", "search:timeline",
		},
		PluginScopes: PluginScopeMap{
			"use": {
				{PluginType: "search_command", PluginCode: "stats"},
			},
		},
	},
}

func BuiltinRBACPermissions() []BuiltinPermission {
	items := make([]BuiltinPermission, len(builtinRBACPermissions))
	copy(items, builtinRBACPermissions)
	return items
}

func BuiltinRBACRoles() []BuiltinRole {
	items := make([]BuiltinRole, len(builtinRBACRoles))
	copy(items, builtinRBACRoles)
	return items
}

func allBuiltinPluginScopes() PluginScopeMap {
	return PluginScopeMap{
		"use": {
			{PluginType: "input", PluginCode: "*"},
			{PluginType: "parser", PluginCode: "*"},
			{PluginType: "search_command", PluginCode: "*"},
		},
		"manage": {
			{PluginType: "input", PluginCode: "*"},
			{PluginType: "parser", PluginCode: "*"},
			{PluginType: "search_command", PluginCode: "*"},
		},
	}
}

func allBuiltinPermissionCodes() []string {
	items := make([]string, 0, len(builtinRBACPermissions))
	for _, item := range builtinRBACPermissions {
		items = append(items, item.Code)
	}
	sort.Strings(items)
	return items
}

func supportedIndexScopeAction(action string) bool {
	action = strings.TrimSpace(strings.ToLower(action))
	return slices.Contains(supportedIndexScopeActions, action)
}

func supportedPluginScopeAction(action string) bool {
	action = strings.TrimSpace(strings.ToLower(action))
	return slices.Contains(supportedPluginScopeActions, action)
}

func supportedPluginScopeType(pluginType string) bool {
	pluginType = strings.TrimSpace(strings.ToLower(pluginType))
	return slices.Contains(supportedPluginScopeTypes, pluginType)
}

func normalizeIndexPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(strings.ToLower(pattern))
	if pattern == "" {
		return "", fmt.Errorf("index pattern is required")
	}
	if pattern == "*" {
		return pattern, nil
	}
	if !indexScopePatternRegexp.MatchString(pattern) {
		return "", fmt.Errorf("invalid index pattern %q", pattern)
	}
	if strings.HasSuffix(pattern, "*") {
		base := strings.TrimSuffix(pattern, "*")
		if base == "" {
			return "", fmt.Errorf("invalid index pattern %q", pattern)
		}
		return pattern, nil
	}
	normalized, err := ch.NormalizeIndexName(pattern)
	if err != nil {
		return "", err
	}
	return normalized, nil
}

func normalizeIndexScopes(input IndexScopeMap) (IndexScopeMap, error) {
	if len(input) == 0 {
		return nil, nil
	}
	result := make(IndexScopeMap, len(input))
	for rawAction, rawPatterns := range input {
		action := strings.TrimSpace(strings.ToLower(rawAction))
		if !supportedIndexScopeAction(action) {
			return nil, fmt.Errorf("unsupported index scope action %q", rawAction)
		}
		patterns := make([]string, 0, len(rawPatterns))
		for _, rawPattern := range rawPatterns {
			pattern, err := normalizeIndexPattern(rawPattern)
			if err != nil {
				return nil, err
			}
			patterns = append(patterns, pattern)
		}
		patterns = uniqueStrings(patterns)
		if len(patterns) > 0 {
			result[action] = patterns
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func normalizePluginScopeBinding(binding PluginScopeBinding) (PluginScopeBinding, error) {
	pluginType := strings.TrimSpace(strings.ToLower(binding.PluginType))
	if !supportedPluginScopeType(pluginType) {
		return PluginScopeBinding{}, fmt.Errorf("unsupported plugin type %q", binding.PluginType)
	}
	pluginCode := strings.TrimSpace(strings.ToLower(binding.PluginCode))
	if pluginCode == "" {
		return PluginScopeBinding{}, fmt.Errorf("plugin code is required")
	}
	if pluginCode != "*" && !pluginScopeCodeRegexp.MatchString(pluginCode) {
		return PluginScopeBinding{}, fmt.Errorf("invalid plugin code %q", binding.PluginCode)
	}
	return PluginScopeBinding{PluginType: pluginType, PluginCode: pluginCode}, nil
}

func normalizePluginScopes(input PluginScopeMap) (PluginScopeMap, error) {
	if len(input) == 0 {
		return nil, nil
	}
	result := make(PluginScopeMap, len(input))
	for rawAction, rawBindings := range input {
		action := strings.TrimSpace(strings.ToLower(rawAction))
		if !supportedPluginScopeAction(action) {
			return nil, fmt.Errorf("unsupported plugin scope action %q", rawAction)
		}
		items := make([]PluginScopeBinding, 0, len(rawBindings))
		for _, rawBinding := range rawBindings {
			item, err := normalizePluginScopeBinding(rawBinding)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		items = uniquePluginScopeBindings(items)
		if len(items) > 0 {
			result[action] = items
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func matchIndexPattern(pattern string, indexName string) bool {
	pattern = strings.TrimSpace(strings.ToLower(pattern))
	indexName = strings.TrimSpace(strings.ToLower(indexName))
	switch {
	case pattern == "*":
		return true
	case strings.HasSuffix(pattern, "*"):
		return strings.HasPrefix(indexName, strings.TrimSuffix(pattern, "*"))
	default:
		return pattern == indexName
	}
}

func indexPatternWithinScope(child string, parent string) bool {
	child = strings.TrimSpace(strings.ToLower(child))
	parent = strings.TrimSpace(strings.ToLower(parent))
	switch {
	case parent == "*":
		return true
	case strings.HasSuffix(child, "*") && strings.HasSuffix(parent, "*"):
		return strings.HasPrefix(strings.TrimSuffix(child, "*"), strings.TrimSuffix(parent, "*"))
	case strings.HasSuffix(child, "*"):
		return false
	default:
		return matchIndexPattern(parent, child)
	}
}

func indexPatternAllowedByAny(pattern string, allowed []string) bool {
	for _, item := range allowed {
		if indexPatternWithinScope(pattern, item) {
			return true
		}
	}
	return false
}

func pluginScopeBindingMatches(binding PluginScopeBinding, pluginType string, pluginCode string) bool {
	pluginType = strings.TrimSpace(strings.ToLower(pluginType))
	pluginCode = strings.TrimSpace(strings.ToLower(pluginCode))
	return binding.PluginType == pluginType && (binding.PluginCode == "*" || binding.PluginCode == pluginCode || pluginCode == "")
}

func pluginScopeBindingWithinScope(child PluginScopeBinding, parent PluginScopeBinding) bool {
	child.PluginType = strings.TrimSpace(strings.ToLower(child.PluginType))
	child.PluginCode = strings.TrimSpace(strings.ToLower(child.PluginCode))
	parent.PluginType = strings.TrimSpace(strings.ToLower(parent.PluginType))
	parent.PluginCode = strings.TrimSpace(strings.ToLower(parent.PluginCode))
	if child.PluginType != parent.PluginType {
		return false
	}
	if parent.PluginCode == "*" {
		return true
	}
	if child.PluginCode == "*" {
		return false
	}
	return child.PluginCode == parent.PluginCode
}

func pluginScopeBindingAllowedByAny(binding PluginScopeBinding, allowed []PluginScopeBinding) bool {
	for _, item := range allowed {
		if pluginScopeBindingWithinScope(binding, item) {
			return true
		}
	}
	return false
}

func computeEffectiveIndexScopes(userScopes IndexScopeMap, tokenScopes IndexScopeMap) map[string]EffectiveIndexScope {
	result := map[string]EffectiveIndexScope{}
	userHasScopes := len(userScopes) > 0
	tokenHasScopes := len(tokenScopes) > 0
	for _, action := range supportedIndexScopeActions {
		userPatterns, userRestricted := userScopes[action]
		tokenPatterns, tokenRestricted := tokenScopes[action]
		switch {
		case tokenRestricted:
			result[action] = EffectiveIndexScope{Restricted: true, Patterns: append([]string(nil), tokenPatterns...)}
		case tokenHasScopes:
			result[action] = EffectiveIndexScope{Restricted: true, Patterns: []string{}}
		case userRestricted:
			result[action] = EffectiveIndexScope{Restricted: true, Patterns: append([]string(nil), userPatterns...)}
		case userHasScopes:
			result[action] = EffectiveIndexScope{Restricted: true, Patterns: []string{}}
		default:
			result[action] = EffectiveIndexScope{Restricted: false, Patterns: []string{}}
		}
	}
	return result
}

func computeEffectivePluginScopes(userScopes PluginScopeMap, tokenScopes PluginScopeMap) PluginScopeMap {
	result := PluginScopeMap{}
	tokenHasScopes := len(tokenScopes) > 0
	for _, action := range supportedPluginScopeActions {
		userItems := uniquePluginScopeBindings(userScopes[action])
		tokenItems := uniquePluginScopeBindings(tokenScopes[action])
		switch {
		case len(tokenItems) > 0:
			result[action] = append([]PluginScopeBinding(nil), tokenItems...)
		case tokenHasScopes:
			result[action] = []PluginScopeBinding{}
		case len(userItems) > 0:
			result[action] = append([]PluginScopeBinding(nil), userItems...)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeRBACPage(page int, pageSize int) RBACListPage {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return RBACListPage{Page: page, PageSize: pageSize}
}

func (p RBACListPage) offset() int {
	return (p.Page - 1) * p.PageSize
}

func finalizeRBACPage(page RBACListPage, total int) RBACListPage {
	page.Total = total
	if page.PageSize <= 0 {
		page.PageSize = 20
	}
	page.TotalPages = 0
	if total > 0 {
		page.TotalPages = (total + page.PageSize - 1) / page.PageSize
	}
	return page
}

func (c *Client) EnsureRBACSeeds(ctx context.Context) error {
	if c == nil || c.db == nil {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE tp FROM auth_token_permissions tp JOIN auth_permissions p ON p.id = tp.permission_id WHERE p.resource_code = 'plugin'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE rp FROM auth_role_permissions rp JOIN auth_permissions p ON p.id = rp.permission_id WHERE p.resource_code = 'plugin'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_permissions WHERE resource_code = 'plugin'`); err != nil {
		return err
	}

	for _, perm := range builtinRBACPermissions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_permissions (id, permission_code, resource_code, action_code, display_name, description, status, builtin) VALUES (?, ?, ?, ?, ?, ?, 'active', 1) ON DUPLICATE KEY UPDATE resource_code = VALUES(resource_code), action_code = VALUES(action_code), display_name = VALUES(display_name), description = VALUES(description), status = 'active', builtin = 1, updated_at = CURRENT_TIMESTAMP(3)`,
			newRBACUUID(), perm.Code, perm.Resource, perm.Action, perm.DisplayName, perm.Description); err != nil {
			return err
		}
	}

	for _, role := range builtinRBACRoles {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_roles (id, role_code, role_name, description, status, builtin) VALUES (?, ?, ?, ?, 'active', 1) ON DUPLICATE KEY UPDATE role_name = VALUES(role_name), description = VALUES(description), status = 'active', builtin = 1, deleted_at = NULL, updated_at = CURRENT_TIMESTAMP(3)`,
			newRBACUUID(), role.Code, role.Name, role.Description); err != nil {
			return err
		}
		var roleID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM auth_roles WHERE role_code = ? AND deleted_at IS NULL`, role.Code).Scan(&roleID); err != nil {
			return err
		}
		for _, permCode := range role.PermissionCodes {
			if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO auth_role_permissions (id, role_id, permission_id) SELECT ?, ?, p.id FROM auth_permissions p WHERE p.permission_code = ?`, newRBACUUID(), roleID, permCode); err != nil {
				return err
			}
		}
		if err := c.replaceRolePluginScopesTx(ctx, tx, roleID, role.PluginScopes, ""); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO auth_user_roles (id, user_id, role_id) SELECT ?, u.id, r.id FROM auth_users u JOIN auth_roles r ON r.role_code = 'platform_admin' WHERE u.role_label = 'admin' AND u.deleted_at IS NULL`, newRBACUUID()); err != nil {
		return err
	}

	return tx.Commit()
}

func (c *Client) ResolvePrincipalByTokenHash(ctx context.Context, tokenHash string) (ResolvedPrincipalRecord, error) {
	var record ResolvedPrincipalRecord
	if c == nil || c.db == nil {
		return record, sql.ErrNoRows
	}
	var expiresAt sql.NullTime
	if err := c.db.QueryRowContext(ctx, `SELECT usr.id, usr.username, COALESCE(usr.display_name, ''), usr.status, tok.id, tok.token_name, tok.status, tok.source, tok.expires_at FROM auth_tokens tok JOIN auth_users usr ON usr.id = tok.user_id WHERE tok.token_hash = ? AND tok.status = 'active' AND usr.status = 'active' AND usr.deleted_at IS NULL AND (tok.expires_at IS NULL OR tok.expires_at > CURRENT_TIMESTAMP(3))`, tokenHash).Scan(&record.UserID, &record.Username, &record.DisplayName, &record.UserStatus, &record.TokenID, &record.TokenName, &record.TokenStatus, &record.TokenSource, &expiresAt); err != nil {
		return record, err
	}
	if expiresAt.Valid {
		record.TokenExpiresAt = &expiresAt.Time
	}

	roles, err := c.listUserRoles(ctx, record.UserID)
	if err != nil {
		return record, err
	}
	record.Roles = roles

	userPerms, err := c.listUserPermissionCodes(ctx, record.UserID)
	if err != nil {
		return record, err
	}
	record.UserPermissionCodes = userPerms

	tokenPerms, err := c.listTokenPermissionCodes(ctx, record.TokenID)
	if err != nil {
		return record, err
	}
	record.TokenPermissionCodes = tokenPerms
	record.EffectivePermissions = computeEffectivePermissionCodes(userPerms, tokenPerms)
	userIndexScopes, err := c.listUserIndexScopes(ctx, record.UserID)
	if err != nil {
		return record, err
	}
	record.UserIndexScopes = userIndexScopes
	tokenIndexScopes, err := c.listTokenIndexScopes(ctx, record.TokenID)
	if err != nil {
		return record, err
	}
	record.TokenIndexScopes = tokenIndexScopes
	record.EffectiveIndexScopes = computeEffectiveIndexScopes(userIndexScopes, tokenIndexScopes)
	userPluginScopes, err := c.listUserPluginScopes(ctx, record.UserID)
	if err != nil {
		return record, err
	}
	record.UserPluginScopes = userPluginScopes
	tokenPluginScopes, err := c.listTokenPluginScopes(ctx, record.TokenID)
	if err != nil {
		return record, err
	}
	record.TokenPluginScopes = tokenPluginScopes
	record.EffectivePluginScopes = computeEffectivePluginScopes(userPluginScopes, tokenPluginScopes)

	_, _ = c.db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at = CURRENT_TIMESTAMP(3) WHERE id = ?`, record.TokenID)
	return record, nil
}

func (c *Client) ListUsers(ctx context.Context, filter UserFilter) ([]AuthUserRecord, RBACListPage, error) {
	page := normalizeRBACPage(filter.Page, filter.PageSize)
	where := []string{"deleted_at IS NULL"}
	args := make([]any, 0, 4)
	if q := strings.TrimSpace(filter.Query); q != "" {
		where = append(where, "(username LIKE ? OR display_name LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	queryWhere := strings.Join(where, " AND ")

	var total int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users WHERE `+queryWhere, args...).Scan(&total); err != nil {
		return nil, RBACListPage{}, err
	}

	rows, err := c.db.QueryContext(ctx, `SELECT id, username, COALESCE(display_name, ''), status, COALESCE(role_label, ''), last_login_at, failed_login_count, created_at, updated_at FROM auth_users WHERE `+queryWhere+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(args, page.PageSize, page.offset())...)
	if err != nil {
		return nil, RBACListPage{}, err
	}
	defer rows.Close()

	items := []AuthUserRecord{}
	for rows.Next() {
		var item AuthUserRecord
		var lastLogin sql.NullTime
		if err := rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Status, &item.RoleLabel, &lastLogin, &item.FailedLoginCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, RBACListPage{}, err
		}
		if lastLogin.Valid {
			item.LastLoginAt = &lastLogin.Time
		}
		item.Roles, err = c.listUserRoles(ctx, item.ID)
		if err != nil {
			return nil, RBACListPage{}, err
		}
		item.RoleCodes = roleCodesFromRecords(item.Roles)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, RBACListPage{}, err
	}
	return items, finalizeRBACPage(page, total), nil
}

func (c *Client) GetUser(ctx context.Context, id string) (AuthUserRecord, error) {
	var item AuthUserRecord
	var lastLogin sql.NullTime
	err := c.db.QueryRowContext(ctx, `SELECT id, username, COALESCE(display_name, ''), status, COALESCE(role_label, ''), last_login_at, failed_login_count, created_at, updated_at FROM auth_users WHERE id = ? AND deleted_at IS NULL`, id).Scan(&item.ID, &item.Username, &item.DisplayName, &item.Status, &item.RoleLabel, &lastLogin, &item.FailedLoginCount, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if lastLogin.Valid {
		item.LastLoginAt = &lastLogin.Time
	}
	item.Roles, err = c.listUserRoles(ctx, item.ID)
	if err != nil {
		return item, err
	}
	item.RoleCodes = roleCodesFromRecords(item.Roles)
	return item, nil
}

func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (AuthUserRecord, error) {
	var item AuthUserRecord
	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" || password == "" {
		return item, fmt.Errorf("username and password are required")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return item, err
	}
	id := newRBACUUID()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_users (id, username, display_name, password_hash, password_algo, role_label, status) VALUES (?, ?, ?, ?, 'bcrypt', '', ?)`, id, username, displayName, string(passwordHash), status); err != nil {
		return item, err
	}
	if err := c.replaceUserRolesTx(ctx, tx, id, req.RoleIDs); err != nil {
		return item, err
	}
	if err := tx.Commit(); err != nil {
		return item, err
	}
	return c.GetUser(ctx, id)
}

func (c *Client) UpdateUser(ctx context.Context, req UpdateUserRequest) (AuthUserRecord, error) {
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	if _, err := c.db.ExecContext(ctx, `UPDATE auth_users SET display_name = ?, status = ?, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND deleted_at IS NULL`, strings.TrimSpace(req.DisplayName), status, req.ID); err != nil {
		return AuthUserRecord{}, err
	}
	return c.GetUser(ctx, req.ID)
}

func (c *Client) DeleteUser(ctx context.Context, id string) error {
	result, err := c.db.ExecContext(ctx, `UPDATE auth_users SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (c *Client) SetUserPassword(ctx context.Context, userID string, password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return fmt.Errorf("password is required")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	result, err := c.db.ExecContext(ctx, `UPDATE auth_users SET password_hash = ?, password_algo = 'bcrypt', failed_login_count = 0, locked_until = NULL, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND deleted_at IS NULL`, string(passwordHash), userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (c *Client) SetUserRoles(ctx context.Context, userID string, roleIDs []string, actorID string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := c.replaceUserRolesTx(ctx, tx, userID, roleIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) ListRoles(ctx context.Context) ([]AuthRoleRecord, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT r.id, r.role_code, r.role_name, COALESCE(r.description, ''), r.status, r.builtin, r.created_at, r.updated_at, (SELECT COUNT(*) FROM auth_user_roles ur JOIN auth_users u ON u.id = ur.user_id AND u.deleted_at IS NULL WHERE ur.role_id = r.id) AS user_count FROM auth_roles r WHERE r.deleted_at IS NULL ORDER BY r.builtin DESC, r.role_code ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthRoleRecord{}
	for rows.Next() {
		var item AuthRoleRecord
		if err := rows.Scan(&item.ID, &item.RoleCode, &item.RoleName, &item.Description, &item.Status, &item.Builtin, &item.CreatedAt, &item.UpdatedAt, &item.UserCount); err != nil {
			return nil, err
		}
		item.PermissionCodes, err = c.listRolePermissionCodes(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.IndexScopes, err = c.listRoleIndexScopes(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.PluginScopes, err = c.listRolePluginScopes(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) GetRole(ctx context.Context, id string) (AuthRoleRecord, error) {
	var item AuthRoleRecord
	err := c.db.QueryRowContext(ctx, `SELECT id, role_code, role_name, COALESCE(description, ''), status, builtin, created_at, updated_at, (SELECT COUNT(*) FROM auth_user_roles ur JOIN auth_users u ON u.id = ur.user_id AND u.deleted_at IS NULL WHERE ur.role_id = auth_roles.id) AS user_count FROM auth_roles WHERE id = ? AND deleted_at IS NULL`, id).Scan(&item.ID, &item.RoleCode, &item.RoleName, &item.Description, &item.Status, &item.Builtin, &item.CreatedAt, &item.UpdatedAt, &item.UserCount)
	if err != nil {
		return item, err
	}
	item.PermissionCodes, err = c.listRolePermissionCodes(ctx, item.ID)
	if err != nil {
		return item, err
	}
	item.IndexScopes, err = c.listRoleIndexScopes(ctx, item.ID)
	if err != nil {
		return item, err
	}
	item.PluginScopes, err = c.listRolePluginScopes(ctx, item.ID)
	return item, err
}

func (c *Client) CreateRole(ctx context.Context, req CreateRoleRequest) (AuthRoleRecord, error) {
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	id := newRBACUUID()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthRoleRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_roles (id, role_code, role_name, description, status, builtin) VALUES (?, ?, ?, ?, ?, 0)`, id, strings.TrimSpace(req.RoleCode), strings.TrimSpace(req.RoleName), strings.TrimSpace(req.Description), status); err != nil {
		return AuthRoleRecord{}, err
	}
	if err := c.replaceRolePermissionsTx(ctx, tx, id, req.PermissionCodes); err != nil {
		return AuthRoleRecord{}, err
	}
	if err := c.replaceRoleIndexScopesTx(ctx, tx, id, req.IndexScopes, req.ActorID); err != nil {
		return AuthRoleRecord{}, err
	}
	if err := c.replaceRolePluginScopesTx(ctx, tx, id, req.PluginScopes, req.ActorID); err != nil {
		return AuthRoleRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthRoleRecord{}, err
	}
	return c.GetRole(ctx, id)
}

func (c *Client) UpdateRole(ctx context.Context, req UpdateRoleRequest) (AuthRoleRecord, error) {
	var builtin bool
	if err := c.db.QueryRowContext(ctx, `SELECT builtin FROM auth_roles WHERE id = ? AND deleted_at IS NULL`, req.ID).Scan(&builtin); err != nil {
		return AuthRoleRecord{}, err
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthRoleRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE auth_roles SET role_name = ?, description = ?, status = ?, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND deleted_at IS NULL`, strings.TrimSpace(req.RoleName), strings.TrimSpace(req.Description), status, req.ID); err != nil {
		return AuthRoleRecord{}, err
	}
	if !builtin || len(req.PermissionCodes) > 0 {
		if err := c.replaceRolePermissionsTx(ctx, tx, req.ID, req.PermissionCodes); err != nil {
			return AuthRoleRecord{}, err
		}
	}
	if err := c.replaceRoleIndexScopesTx(ctx, tx, req.ID, req.IndexScopes, req.ActorID); err != nil {
		return AuthRoleRecord{}, err
	}
	if err := c.replaceRolePluginScopesTx(ctx, tx, req.ID, req.PluginScopes, req.ActorID); err != nil {
		return AuthRoleRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return AuthRoleRecord{}, err
	}
	return c.GetRole(ctx, req.ID)
}

func (c *Client) DeleteRole(ctx context.Context, id string) error {
	var builtin bool
	if err := c.db.QueryRowContext(ctx, `SELECT builtin FROM auth_roles WHERE id = ? AND deleted_at IS NULL`, id).Scan(&builtin); err != nil {
		return err
	}
	if builtin {
		return fmt.Errorf("builtin role cannot be deleted")
	}
	var refs int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_user_roles ur JOIN auth_users u ON u.id = ur.user_id AND u.deleted_at IS NULL WHERE ur.role_id = ?`, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("role is still referenced by users")
	}
	result, err := c.db.ExecContext(ctx, `UPDATE auth_roles SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (c *Client) ListPermissions(ctx context.Context) ([]AuthPermissionRecord, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id, permission_code, resource_code, action_code, display_name, COALESCE(description, ''), status, builtin, created_at, updated_at FROM auth_permissions WHERE status = 'active' ORDER BY resource_code ASC, action_code ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthPermissionRecord{}
	for rows.Next() {
		var item AuthPermissionRecord
		if err := rows.Scan(&item.ID, &item.PermissionCode, &item.ResourceCode, &item.ActionCode, &item.DisplayName, &item.Description, &item.Status, &item.Builtin, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) ListTokens(ctx context.Context, userID string) ([]AuthTokenRecord, error) {
	query := `SELECT tok.id, tok.user_id, usr.username, tok.token_name, COALESCE(tok.token_prefix, ''), tok.token_type, tok.source, tok.status, tok.expires_at, tok.last_used_at, tok.revoked_at, tok.created_at, tok.updated_at FROM auth_tokens tok JOIN auth_users usr ON usr.id = tok.user_id WHERE usr.deleted_at IS NULL`
	args := []any{}
	if strings.TrimSpace(userID) != "" {
		query += ` AND tok.user_id = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY tok.created_at DESC`
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthTokenRecord{}
	for rows.Next() {
		var item AuthTokenRecord
		var expiresAt, lastUsedAt, revokedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.TokenName, &item.TokenPrefix, &item.TokenType, &item.Source, &item.Status, &expiresAt, &lastUsedAt, &revokedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.Time
		}
		if lastUsedAt.Valid {
			item.LastUsedAt = &lastUsedAt.Time
		}
		if revokedAt.Valid {
			item.RevokedAt = &revokedAt.Time
		}
		item.PermissionCodes, err = c.listTokenPermissionCodes(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.IndexScopes, err = c.listTokenIndexScopes(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.PluginScopes, err = c.listTokenPluginScopes(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) CreateScopedToken(ctx context.Context, req CreateScopedTokenRequest) (CreatedTokenRecord, error) {
	var result CreatedTokenRecord
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.TokenName) == "" {
		return result, fmt.Errorf("user_id and token_name are required")
	}
	var count int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users WHERE id = ? AND status = 'active' AND deleted_at IS NULL`, req.UserID).Scan(&count); err != nil {
		return result, err
	}
	if count == 0 {
		return result, sql.ErrNoRows
	}
	userPerms, err := c.listUserPermissionCodes(ctx, req.UserID)
	if err != nil {
		return result, err
	}
	requested := uniqueStrings(req.PermissionCodes)
	userPermSet := sliceToSet(userPerms)
	for _, code := range requested {
		if _, ok := userPermSet[code]; !ok {
			return result, fmt.Errorf("permission %s exceeds user scope", code)
		}
	}
	userIndexScopes, err := c.listUserIndexScopes(ctx, req.UserID)
	if err != nil {
		return result, err
	}
	normalizedIndexScopes, err := normalizeIndexScopes(req.IndexScopes)
	if err != nil {
		return result, err
	}
	userPluginScopes, err := c.listUserPluginScopes(ctx, req.UserID)
	if err != nil {
		return result, err
	}
	normalizedPluginScopes, err := normalizePluginScopes(req.PluginScopes)
	if err != nil {
		return result, err
	}
	for action, patterns := range normalizedIndexScopes {
		if allowed, restricted := userIndexScopes[action]; restricted {
			for _, pattern := range patterns {
				if !indexPatternAllowedByAny(pattern, allowed) {
					return result, fmt.Errorf("index scope %s=%s exceeds user scope", action, pattern)
				}
			}
		}
	}
	for action, bindings := range normalizedPluginScopes {
		allowed := userPluginScopes[action]
		for _, binding := range bindings {
			if !pluginScopeBindingAllowedByAny(binding, allowed) {
				return result, fmt.Errorf("plugin scope %s=%s/%s exceeds user scope", action, binding.PluginType, binding.PluginCode)
			}
		}
	}

	plaintext, err := newAPIPlaintextToken()
	if err != nil {
		return result, err
	}
	tokenID := newRBACUUID()
	tokenHash := hashRBACSecret(plaintext)
	tokenPrefix := plaintext
	if len(tokenPrefix) > 8 {
		tokenPrefix = tokenPrefix[:8]
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_tokens (id, user_id, token_name, token_hash, token_prefix, token_type, source, status, expires_at) VALUES (?, ?, ?, ?, ?, 'bearer', 'api', 'active', ?)`, tokenID, req.UserID, strings.TrimSpace(req.TokenName), tokenHash, tokenPrefix, req.ExpiresAt); err != nil {
		return result, err
	}
	if err := c.replaceTokenPermissionsTx(ctx, tx, tokenID, requested); err != nil {
		return result, err
	}
	if err := c.replaceTokenIndexScopesTx(ctx, tx, tokenID, normalizedIndexScopes, req.ActorID); err != nil {
		return result, err
	}
	if err := c.replaceTokenPluginScopesTx(ctx, tx, tokenID, normalizedPluginScopes, req.ActorID); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	effectiveIndexScopes := computeEffectiveIndexScopes(userIndexScopes, normalizedIndexScopes)
	result = CreatedTokenRecord{
		TokenID:               tokenID,
		UserID:                req.UserID,
		TokenName:             strings.TrimSpace(req.TokenName),
		TokenPrefix:           tokenPrefix,
		Plaintext:             plaintext,
		ExpiresAt:             req.ExpiresAt,
		EffectiveScopes:       computeEffectivePermissionCodes(userPerms, requested),
		EffectivePermissions:  computeEffectivePermissionCodes(userPerms, requested),
		EffectiveIndexScopes:  effectiveIndexScopes,
		EffectivePluginScopes: computeEffectivePluginScopes(userPluginScopes, normalizedPluginScopes),
	}
	return result, nil
}

func (c *Client) RevokeToken(ctx context.Context, tokenID string, actorID string) error {
	result, err := c.db.ExecContext(ctx, `UPDATE auth_tokens SET status = 'revoked', revoked_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND status <> 'revoked'`, tokenID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (c *Client) AppendAuthAuditLog(ctx context.Context, item AuthAuditLogRecord) error {
	if c == nil || c.db == nil {
		return nil
	}
	metadata := item.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	if _, err := c.db.ExecContext(ctx, `INSERT INTO auth_audit_logs (id, user_id, username, event_type, result, request_id, source_ip, user_agent, method, path, error_code, metadata) VALUES (?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?)`, newRBACUUID(), item.UserID, item.Username, item.EventType, item.Result, item.RequestID, item.SourceIP, item.UserAgent, item.Method, item.Path, item.ErrorCode, metadata); err != nil {
		return err
	}
	return nil
}

func (c *Client) ListAuthAuditLogs(ctx context.Context, page int, pageSize int) ([]AuthAuditLogRecord, RBACListPage, error) {
	p := normalizeRBACPage(page, pageSize)
	var total int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_audit_logs`).Scan(&total); err != nil {
		return nil, RBACListPage{}, err
	}
	rows, err := c.db.QueryContext(ctx, `SELECT id, COALESCE(user_id, ''), COALESCE(username, ''), event_type, result, COALESCE(request_id, ''), COALESCE(source_ip, ''), COALESCE(user_agent, ''), COALESCE(method, ''), COALESCE(path, ''), COALESCE(error_code, ''), metadata, created_at FROM auth_audit_logs ORDER BY created_at DESC LIMIT ? OFFSET ?`, p.PageSize, p.offset())
	if err != nil {
		return nil, RBACListPage{}, err
	}
	defer rows.Close()
	items := []AuthAuditLogRecord{}
	for rows.Next() {
		var item AuthAuditLogRecord
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.EventType, &item.Result, &item.RequestID, &item.SourceIP, &item.UserAgent, &item.Method, &item.Path, &item.ErrorCode, &item.Metadata, &item.CreatedAt); err != nil {
			return nil, RBACListPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, RBACListPage{}, err
	}
	return items, finalizeRBACPage(p, total), nil
}

func (c *Client) listUserRoles(ctx context.Context, userID string) ([]AuthRoleRecord, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT r.id, r.role_code, r.role_name, COALESCE(r.description, ''), r.status, r.builtin, r.created_at, r.updated_at FROM auth_roles r JOIN auth_user_roles ur ON ur.role_id = r.id WHERE ur.user_id = ? AND r.deleted_at IS NULL AND r.status = 'active' ORDER BY r.builtin DESC, r.role_code ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthRoleRecord{}
	for rows.Next() {
		var item AuthRoleRecord
		if err := rows.Scan(&item.ID, &item.RoleCode, &item.RoleName, &item.Description, &item.Status, &item.Builtin, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func roleCodesFromRecords(items []AuthRoleRecord) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.RoleCode)
	}
	sort.Strings(result)
	return result
}

func (c *Client) listUserPermissionCodes(ctx context.Context, userID string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT p.permission_code FROM auth_permissions p JOIN auth_role_permissions rp ON rp.permission_id = p.id JOIN auth_user_roles ur ON ur.role_id = rp.role_id JOIN auth_roles r ON r.id = ur.role_id WHERE ur.user_id = ? AND p.status = 'active' AND r.status = 'active' AND r.deleted_at IS NULL ORDER BY p.permission_code ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSingleColumnStrings(rows)
}

func (c *Client) listRolePermissionCodes(ctx context.Context, roleID string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT p.permission_code FROM auth_permissions p JOIN auth_role_permissions rp ON rp.permission_id = p.id WHERE rp.role_id = ? AND p.status = 'active' ORDER BY p.permission_code ASC`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSingleColumnStrings(rows)
}

func (c *Client) listTokenPermissionCodes(ctx context.Context, tokenID string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT p.permission_code FROM auth_permissions p JOIN auth_token_permissions tp ON tp.permission_id = p.id WHERE tp.token_id = ? AND p.status = 'active' ORDER BY p.permission_code ASC`, tokenID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSingleColumnStrings(rows)
}

func (c *Client) listRoleIndexScopes(ctx context.Context, roleID string) (IndexScopeMap, error) {
	return c.listIndexScopes(ctx, `SELECT scope_action, index_pattern FROM auth_role_index_scopes WHERE role_id = ? ORDER BY scope_action ASC, index_pattern ASC`, roleID)
}

func (c *Client) listUserIndexScopes(ctx context.Context, userID string) (IndexScopeMap, error) {
	return c.listIndexScopes(ctx, `SELECT DISTINCT ris.scope_action, ris.index_pattern FROM auth_role_index_scopes ris JOIN auth_user_roles ur ON ur.role_id = ris.role_id JOIN auth_roles r ON r.id = ur.role_id WHERE ur.user_id = ? AND r.deleted_at IS NULL AND r.status = 'active' ORDER BY ris.scope_action ASC, ris.index_pattern ASC`, userID)
}

func (c *Client) listTokenIndexScopes(ctx context.Context, tokenID string) (IndexScopeMap, error) {
	return c.listIndexScopes(ctx, `SELECT scope_action, index_pattern FROM auth_token_index_scopes WHERE token_id = ? ORDER BY scope_action ASC, index_pattern ASC`, tokenID)
}

func (c *Client) listRolePluginScopes(ctx context.Context, roleID string) (PluginScopeMap, error) {
	return c.listPluginScopes(ctx, `SELECT scope_action, plugin_type, plugin_code FROM auth_role_plugin_scopes WHERE role_id = ? ORDER BY scope_action ASC, plugin_type ASC, plugin_code ASC`, roleID)
}

func (c *Client) listUserPluginScopes(ctx context.Context, userID string) (PluginScopeMap, error) {
	return c.listPluginScopes(ctx, `SELECT DISTINCT rps.scope_action, rps.plugin_type, rps.plugin_code FROM auth_role_plugin_scopes rps JOIN auth_user_roles ur ON ur.role_id = rps.role_id JOIN auth_roles r ON r.id = ur.role_id WHERE ur.user_id = ? AND r.deleted_at IS NULL AND r.status = 'active' ORDER BY rps.scope_action ASC, rps.plugin_type ASC, rps.plugin_code ASC`, userID)
}

func (c *Client) listTokenPluginScopes(ctx context.Context, tokenID string) (PluginScopeMap, error) {
	return c.listPluginScopes(ctx, `SELECT scope_action, plugin_type, plugin_code FROM auth_token_plugin_scopes WHERE token_id = ? ORDER BY scope_action ASC, plugin_type ASC, plugin_code ASC`, tokenID)
}

func (c *Client) listIndexScopes(ctx context.Context, query string, args ...any) (IndexScopeMap, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := IndexScopeMap{}
	for rows.Next() {
		var action string
		var pattern string
		if err := rows.Scan(&action, &pattern); err != nil {
			return nil, err
		}
		action = strings.TrimSpace(strings.ToLower(action))
		pattern = strings.TrimSpace(strings.ToLower(pattern))
		result[action] = append(result[action], pattern)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for action, patterns := range result {
		result[action] = uniqueStrings(patterns)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func (c *Client) listPluginScopes(ctx context.Context, query string, args ...any) (PluginScopeMap, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := PluginScopeMap{}
	for rows.Next() {
		var action string
		var pluginType string
		var pluginCode string
		if err := rows.Scan(&action, &pluginType, &pluginCode); err != nil {
			return nil, err
		}
		action = strings.TrimSpace(strings.ToLower(action))
		result[action] = append(result[action], PluginScopeBinding{
			PluginType: strings.TrimSpace(strings.ToLower(pluginType)),
			PluginCode: strings.TrimSpace(strings.ToLower(pluginCode)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for action, items := range result {
		result[action] = uniquePluginScopeBindings(items)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func scanSingleColumnStrings(rows *sql.Rows) ([]string, error) {
	items := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(items)
	return uniqueStrings(items), nil
}

func (c *Client) replaceUserRolesTx(ctx context.Context, tx *sql.Tx, userID string, roleIDs []string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users WHERE id = ? AND deleted_at IS NULL`, userID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	roleIDs = uniqueStrings(roleIDs)
	if len(roleIDs) > 0 {
		query, args := inClauseQuery(`SELECT COUNT(*) FROM auth_roles WHERE deleted_at IS NULL AND status = 'active' AND id IN (?)`, roleIDs)
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return err
		}
		if count != len(roleIDs) {
			return fmt.Errorf("one or more role_ids are invalid")
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_user_roles WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_user_roles (id, user_id, role_id) VALUES (?, ?, ?)`, newRBACUUID(), userID, roleID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) replaceRolePermissionsTx(ctx context.Context, tx *sql.Tx, roleID string, permissionCodes []string) error {
	permissionCodes = uniqueStrings(permissionCodes)
	if len(permissionCodes) > 0 {
		query, args := inClauseQuery(`SELECT COUNT(*) FROM auth_permissions WHERE status = 'active' AND permission_code IN (?)`, permissionCodes)
		var count int
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return err
		}
		if count != len(permissionCodes) {
			return fmt.Errorf("one or more permission_codes are invalid")
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	for _, code := range permissionCodes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_role_permissions (id, role_id, permission_id) SELECT ?, ?, id FROM auth_permissions WHERE permission_code = ?`, newRBACUUID(), roleID, code); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) replaceTokenPermissionsTx(ctx context.Context, tx *sql.Tx, tokenID string, permissionCodes []string) error {
	permissionCodes = uniqueStrings(permissionCodes)
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_token_permissions WHERE token_id = ?`, tokenID); err != nil {
		return err
	}
	for _, code := range permissionCodes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_token_permissions (id, token_id, permission_id) SELECT ?, ?, id FROM auth_permissions WHERE permission_code = ?`, newRBACUUID(), tokenID, code); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) replaceRoleIndexScopesTx(ctx context.Context, tx *sql.Tx, roleID string, indexScopes IndexScopeMap, actorID string) error {
	normalized, err := normalizeIndexScopes(indexScopes)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_role_index_scopes WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	for _, action := range supportedIndexScopeActions {
		patterns := normalized[action]
		for _, pattern := range patterns {
			if _, err := tx.ExecContext(ctx, `INSERT INTO auth_role_index_scopes (id, role_id, scope_action, index_pattern, created_by) VALUES (?, ?, ?, ?, NULLIF(?, ''))`, newRBACUUID(), roleID, action, pattern, actorID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) replaceTokenIndexScopesTx(ctx context.Context, tx *sql.Tx, tokenID string, indexScopes IndexScopeMap, actorID string) error {
	normalized, err := normalizeIndexScopes(indexScopes)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_token_index_scopes WHERE token_id = ?`, tokenID); err != nil {
		return err
	}
	for _, action := range supportedIndexScopeActions {
		patterns := normalized[action]
		for _, pattern := range patterns {
			if _, err := tx.ExecContext(ctx, `INSERT INTO auth_token_index_scopes (id, token_id, scope_action, index_pattern, created_by) VALUES (?, ?, ?, ?, NULLIF(?, ''))`, newRBACUUID(), tokenID, action, pattern, actorID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) replaceRolePluginScopesTx(ctx context.Context, tx *sql.Tx, roleID string, pluginScopes PluginScopeMap, actorID string) error {
	normalized, err := normalizePluginScopes(pluginScopes)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_role_plugin_scopes WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	for _, action := range supportedPluginScopeActions {
		items := normalized[action]
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `INSERT INTO auth_role_plugin_scopes (id, role_id, scope_action, plugin_type, plugin_code, created_by) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''))`, newRBACUUID(), roleID, action, item.PluginType, item.PluginCode, actorID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) replaceTokenPluginScopesTx(ctx context.Context, tx *sql.Tx, tokenID string, pluginScopes PluginScopeMap, actorID string) error {
	normalized, err := normalizePluginScopes(pluginScopes)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_token_plugin_scopes WHERE token_id = ?`, tokenID); err != nil {
		return err
	}
	for _, action := range supportedPluginScopeActions {
		items := normalized[action]
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `INSERT INTO auth_token_plugin_scopes (id, token_id, scope_action, plugin_type, plugin_code, created_by) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''))`, newRBACUUID(), tokenID, action, item.PluginType, item.PluginCode, actorID); err != nil {
				return err
			}
		}
	}
	return nil
}

func computeEffectivePermissionCodes(userPerms []string, tokenPerms []string) []string {
	userPerms = uniqueStrings(userPerms)
	tokenPerms = uniqueStrings(tokenPerms)
	if len(tokenPerms) == 0 {
		return userPerms
	}
	set := sliceToSet(tokenPerms)
	result := make([]string, 0, len(userPerms))
	for _, code := range userPerms {
		if _, ok := set[code]; ok {
			result = append(result, code)
		}
	}
	sort.Strings(result)
	return result
}

func sliceToSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return set
}

func uniqueStrings(values []string) []string {
	set := sliceToSet(values)
	items := make([]string, 0, len(set))
	for value := range set {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func uniquePluginScopeBindings(values []PluginScopeBinding) []PluginScopeBinding {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]PluginScopeBinding{}
	for _, value := range values {
		key := strings.TrimSpace(strings.ToLower(value.PluginType)) + "/" + strings.TrimSpace(strings.ToLower(value.PluginCode))
		seen[key] = PluginScopeBinding{
			PluginType: strings.TrimSpace(strings.ToLower(value.PluginType)),
			PluginCode: strings.TrimSpace(strings.ToLower(value.PluginCode)),
		}
	}
	items := make([]PluginScopeBinding, 0, len(seen))
	for _, value := range seen {
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].PluginType == items[j].PluginType {
			return items[i].PluginCode < items[j].PluginCode
		}
		return items[i].PluginType < items[j].PluginType
	})
	return items
}

func inClauseQuery(base string, values []string) (string, []any) {
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, value := range values {
		placeholders = append(placeholders, "?")
		args = append(args, value)
	}
	return strings.Replace(base, "(?)", "("+strings.Join(placeholders, ",")+")", 1), args
}

func newRBACUUID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1_000_000_000_000)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

func newAPIPlaintextToken() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "xdp_" + hex.EncodeToString(bytes[:]), nil
}

func hashRBACSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func HasBuiltinPermission(code string) bool {
	return slices.Contains(allBuiltinPermissionCodes(), strings.TrimSpace(code))
}
