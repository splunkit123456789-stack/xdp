package mvp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	mysqlstore "xdp/pkg/storage/mysql"
)

type AuthenticatedPrincipal struct {
	UserID          string                                    `json:"user_id"`
	Username        string                                    `json:"username"`
	DisplayName     string                                    `json:"display_name,omitempty"`
	TokenID         string                                    `json:"token_id,omitempty"`
	TokenName       string                                    `json:"token_name,omitempty"`
	TokenSource     string                                    `json:"token_source,omitempty"`
	RoleCodes       []string                                  `json:"role_codes,omitempty"`
	Roles           []mysqlstore.AuthRoleRecord               `json:"roles,omitempty"`
	PermissionCodes []string                                  `json:"permission_codes,omitempty"`
	IndexScopes     map[string]mysqlstore.EffectiveIndexScope `json:"index_scopes,omitempty"`
	PluginScopes    mysqlstore.PluginScopeMap                 `json:"plugin_scopes,omitempty"`
	Permissions     map[string]struct{}                       `json:"-"`
	EnvFallback     bool                                      `json:"env_fallback,omitempty"`
}

type principalContextKey struct{}

type createUserAPIRequest struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Password    string   `json:"password"`
	Status      string   `json:"status"`
	RoleIDs     []string `json:"role_ids"`
}

type updateUserAPIRequest struct {
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

type resetUserPasswordAPIRequest struct {
	Password string `json:"password"`
}

type setUserRolesAPIRequest struct {
	RoleIDs []string `json:"role_ids"`
}

type createRoleAPIRequest struct {
	RoleCode        string                    `json:"role_code"`
	RoleName        string                    `json:"role_name"`
	Description     string                    `json:"description"`
	Status          string                    `json:"status"`
	PermissionCodes []string                  `json:"permission_codes"`
	IndexScopes     mysqlstore.IndexScopeMap  `json:"index_scopes"`
	PluginScopes    mysqlstore.PluginScopeMap `json:"plugin_scopes"`
}

type updateRoleAPIRequest struct {
	RoleName        string                    `json:"role_name"`
	Description     string                    `json:"description"`
	Status          string                    `json:"status"`
	PermissionCodes []string                  `json:"permission_codes"`
	IndexScopes     mysqlstore.IndexScopeMap  `json:"index_scopes"`
	PluginScopes    mysqlstore.PluginScopeMap `json:"plugin_scopes"`
}

type createTokenAPIRequest struct {
	UserID          string                    `json:"user_id"`
	TokenName       string                    `json:"token_name"`
	ExpiresAt       *time.Time                `json:"expires_at"`
	PermissionCodes []string                  `json:"permission_codes"`
	IndexScopes     mysqlstore.IndexScopeMap  `json:"index_scopes"`
	PluginScopes    mysqlstore.PluginScopeMap `json:"plugin_scopes"`
}

func withPrincipal(ctx context.Context, principal AuthenticatedPrincipal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func principalFromContext(ctx context.Context) (AuthenticatedPrincipal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(AuthenticatedPrincipal)
	return principal, ok
}

func (p AuthenticatedPrincipal) Has(permission string) bool {
	if permission == "" {
		return true
	}
	_, ok := p.Permissions[permission]
	return ok
}

func (p AuthenticatedPrincipal) indexScope(action string) mysqlstore.EffectiveIndexScope {
	action = strings.TrimSpace(strings.ToLower(action))
	if len(p.IndexScopes) == 0 {
		return mysqlstore.EffectiveIndexScope{}
	}
	scope, ok := p.IndexScopes[action]
	if !ok {
		return mysqlstore.EffectiveIndexScope{Restricted: true, Patterns: []string{}}
	}
	return scope
}

func (p AuthenticatedPrincipal) AllowsIndex(action string, indexName string) bool {
	scope := p.indexScope(action)
	if !scope.Restricted {
		return true
	}
	for _, pattern := range scope.Patterns {
		if matchesIndexScopePattern(pattern, indexName) {
			return true
		}
	}
	return false
}

func (p AuthenticatedPrincipal) AllowsIndexRead(indexName string) bool {
	return p.AllowsIndex("read", indexName) || p.AllowsIndex("manage", indexName)
}

func (p AuthenticatedPrincipal) RequiresExplicitScopedIndex(action string) bool {
	return p.indexScope(action).Restricted
}

func (p AuthenticatedPrincipal) IsPlatformAdmin() bool {
	for _, code := range p.RoleCodes {
		if code == "platform_admin" {
			return true
		}
	}
	return false
}

func (p AuthenticatedPrincipal) pluginScope(action string) []mysqlstore.PluginScopeBinding {
	action = strings.TrimSpace(strings.ToLower(action))
	if p.PluginScopes == nil {
		return nil
	}
	return p.PluginScopes[action]
}

func (p AuthenticatedPrincipal) AllowsPlugin(action string, pluginType string, pluginCode string) bool {
	if productVisibleBuiltinPlugin(normalizePluginType(pluginType), strings.TrimSpace(strings.ToLower(pluginCode))) {
		return true
	}
	if strings.TrimSpace(strings.ToLower(action)) == "use" && p.allowsPluginExact("manage", pluginType, pluginCode) {
		return true
	}
	return p.allowsPluginExact(action, pluginType, pluginCode)
}

func (p AuthenticatedPrincipal) allowsPluginExact(action string, pluginType string, pluginCode string) bool {
	items := p.pluginScope(action)
	for _, item := range items {
		if item.PluginType == normalizePluginType(pluginType) && (item.PluginCode == "*" || strings.TrimSpace(strings.ToLower(pluginCode)) == "" || item.PluginCode == strings.TrimSpace(strings.ToLower(pluginCode))) {
			return true
		}
	}
	return false
}

func (p AuthenticatedPrincipal) HasAnyPluginScope(action string, pluginType string) bool {
	items := p.pluginScope(action)
	for _, item := range items {
		if item.PluginType == normalizePluginType(pluginType) {
			return true
		}
	}
	return false
}

func (h *Handler) resolvePrincipal(ctx context.Context, token string) (AuthenticatedPrincipal, error) {
	if h.mysql != nil && strings.TrimSpace(token) != "" {
		record, err := h.mysql.ResolvePrincipalByTokenHash(ctx, hashAuthSecret(token))
		if err == nil {
			principal := AuthenticatedPrincipal{
				UserID:          record.UserID,
				Username:        record.Username,
				DisplayName:     record.DisplayName,
				TokenID:         record.TokenID,
				TokenName:       record.TokenName,
				TokenSource:     record.TokenSource,
				Roles:           record.Roles,
				RoleCodes:       roleCodesFromRoles(record.Roles),
				PermissionCodes: append([]string(nil), record.EffectivePermissions...),
				IndexScopes:     record.EffectiveIndexScopes,
				PluginScopes:    record.EffectivePluginScopes,
				Permissions:     sliceToPermissionSet(record.EffectivePermissions),
			}
			return principal, nil
		}
		if err != sql.ErrNoRows {
			h.logger.Warn("resolve principal failed", "error", err)
		}
	}
	if !h.auth.Enabled || strings.TrimSpace(token) == "" || strings.TrimSpace(token) == strings.TrimSpace(h.auth.Token) {
		return h.envPlatformAdminPrincipal(), nil
	}
	return AuthenticatedPrincipal{}, sql.ErrNoRows
}

func (h *Handler) envPlatformAdminPrincipal() AuthenticatedPrincipal {
	role := mysqlstore.AuthRoleRecord{
		RoleCode: "platform_admin",
		RoleName: "平台管理员",
		Status:   "active",
		Builtin:  true,
	}
	codes := allRBACPermissionCodes()
	return AuthenticatedPrincipal{
		UserID:          "env-admin",
		Username:        h.auth.Username,
		DisplayName:     h.auth.Username,
		TokenID:         "env-token",
		TokenName:       "default",
		TokenSource:     "env_seed",
		RoleCodes:       []string{"platform_admin"},
		Roles:           []mysqlstore.AuthRoleRecord{role},
		PermissionCodes: codes,
		PluginScopes:    mysqlstore.PluginScopeMap{"use": {{PluginType: "input", PluginCode: "*"}, {PluginType: "parser", PluginCode: "*"}, {PluginType: "search_command", PluginCode: "*"}}, "manage": {{PluginType: "input", PluginCode: "*"}, {PluginType: "parser", PluginCode: "*"}, {PluginType: "search_command", PluginCode: "*"}}},
		Permissions:     sliceToPermissionSet(codes),
		EnvFallback:     true,
	}
}

func allRBACPermissionCodes() []string {
	perms := mysqlstore.BuiltinRBACPermissions()
	items := make([]string, 0, len(perms))
	for _, item := range perms {
		items = append(items, item.Code)
	}
	sort.Strings(items)
	return items
}

func roleCodesFromRoles(items []mysqlstore.AuthRoleRecord) []string {
	codes := make([]string, 0, len(items))
	for _, item := range items {
		codes = append(codes, item.RoleCode)
	}
	sort.Strings(codes)
	return codes
}

func sliceToPermissionSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return set
}

func matchesIndexScopePattern(pattern string, indexName string) bool {
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

func indexScopePermission(action string) string {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "read":
		return "index:read"
	case "manage":
		return "index:manage"
	case "search":
		return "search:execute"
	default:
		return ""
	}
}

func (h *Handler) principalForRequest(w http.ResponseWriter, r *http.Request) (AuthenticatedPrincipal, bool) {
	if principal, ok := principalFromContext(r.Context()); ok {
		return principal, true
	}
	if !h.auth.Enabled {
		principal := h.envPlatformAdminPrincipal()
		return principal, true
	}
	token := tokenFromRequest(r)
	if strings.TrimSpace(token) == "" {
		writeErrorCode(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return AuthenticatedPrincipal{}, false
	}
	principal, err := h.resolvePrincipal(r.Context(), token)
	if err != nil {
		writeErrorCode(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return AuthenticatedPrincipal{}, false
	}
	return principal, true
}

func (h *Handler) withAuthenticatedPrincipal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := h.principalForRequest(w, r)
		if !ok {
			return
		}
		next(w, r.WithContext(withPrincipal(r.Context(), principal)))
	}
}

func (h *Handler) withPermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := h.principalForRequest(w, r)
		if !ok {
			return
		}
		if !principal.Has(permission) {
			h.writePermissionDenied(w, r, principal, permission)
			return
		}
		next(w, r.WithContext(withPrincipal(r.Context(), principal)))
	}
}

func (h *Handler) withAnyPermission(permissions []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := h.principalForRequest(w, r)
		if !ok {
			return
		}
		for _, permission := range permissions {
			if principal.Has(permission) {
				next(w, r.WithContext(withPrincipal(r.Context(), principal)))
				return
			}
		}
		required := ""
		if len(permissions) > 0 {
			required = permissions[0]
		}
		h.writePermissionDenied(w, r, principal, required)
	}
}

func (h *Handler) withPermissionResolver(resolve func(*http.Request) string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := h.principalForRequest(w, r)
		if !ok {
			return
		}
		permission := strings.TrimSpace(resolve(r))
		if permission == "" || principal.Has(permission) {
			next(w, r.WithContext(withPrincipal(r.Context(), principal)))
			return
		}
		h.writePermissionDenied(w, r, principal, permission)
	}
}

func (h *Handler) writePermissionDenied(w http.ResponseWriter, r *http.Request, principal AuthenticatedPrincipal, requiredPermission string) {
	requestID := newRequestID()
	if h.mysql != nil {
		metadata, _ := json.Marshal(map[string]any{
			"required_permission": requiredPermission,
		})
		_ = h.mysql.AppendAuthAuditLog(r.Context(), mysqlstore.AuthAuditLogRecord{
			UserID:    principal.UserID,
			Username:  principal.Username,
			EventType: "authz_denied",
			Result:    "denied",
			RequestID: requestID,
			SourceIP:  r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Method:    r.Method,
			Path:      r.URL.Path,
			ErrorCode: "FORBIDDEN",
			Metadata:  metadata,
		})
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error": map[string]any{
			"code":                "FORBIDDEN",
			"message":             "permission denied",
			"required_permission": requiredPermission,
		},
		"request_id": requestID,
	})
}

func (h *Handler) writeIndexScopeDenied(w http.ResponseWriter, r *http.Request, principal AuthenticatedPrincipal, action string, indexName string) {
	requestID := newRequestID()
	requiredPermission := indexScopePermission(action)
	requiredScope := "index:" + strings.TrimSpace(strings.ToLower(action))
	metadata, _ := json.Marshal(map[string]any{
		"required_permission": requiredPermission,
		"required_scope":      requiredScope,
		"resource_name":       strings.TrimSpace(indexName),
	})
	if h.mysql != nil {
		_ = h.mysql.AppendAuthAuditLog(r.Context(), mysqlstore.AuthAuditLogRecord{
			UserID:    principal.UserID,
			Username:  principal.Username,
			EventType: "authz_denied",
			Result:    "denied",
			RequestID: requestID,
			SourceIP:  r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Method:    r.Method,
			Path:      r.URL.Path,
			ErrorCode: "FORBIDDEN",
			Metadata:  metadata,
		})
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error": map[string]any{
			"code":                "FORBIDDEN",
			"message":             "permission denied",
			"required_permission": requiredPermission,
			"required_scope":      requiredScope,
			"resource_name":       strings.TrimSpace(indexName),
		},
		"request_id": requestID,
	})
}

func (h *Handler) writePluginScopeDenied(w http.ResponseWriter, r *http.Request, principal AuthenticatedPrincipal, action string, pluginType string, pluginCode string) {
	requestID := newRequestID()
	requiredScope := "plugin:" + strings.TrimSpace(strings.ToLower(action))
	pluginType = normalizePluginType(pluginType)
	pluginCode = strings.TrimSpace(strings.ToLower(pluginCode))
	resourceName := strings.TrimSpace(strings.Trim(pluginType+"/"+pluginCode, "/"))
	metadata, _ := json.Marshal(map[string]any{
		"required_scope": requiredScope,
		"plugin_type":    pluginType,
		"plugin_code":    pluginCode,
		"resource_name":  resourceName,
	})
	if h.mysql != nil {
		_ = h.mysql.AppendAuthAuditLog(r.Context(), mysqlstore.AuthAuditLogRecord{
			UserID:    principal.UserID,
			Username:  principal.Username,
			EventType: "authz_denied",
			Result:    "denied",
			RequestID: requestID,
			SourceIP:  r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Method:    r.Method,
			Path:      r.URL.Path,
			ErrorCode: "FORBIDDEN",
			Metadata:  metadata,
		})
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error": map[string]any{
			"code":           "FORBIDDEN",
			"message":        "permission denied",
			"required_scope": requiredScope,
			"plugin_type":    pluginType,
			"plugin_code":    pluginCode,
			"resource_name":  resourceName,
		},
		"request_id": requestID,
	})
}

func (h *Handler) authorizeIndexScope(w http.ResponseWriter, r *http.Request, principal AuthenticatedPrincipal, action string, indexName string) bool {
	if principal.AllowsIndex(action, indexName) {
		return true
	}
	h.writeIndexScopeDenied(w, r, principal, action, indexName)
	return false
}

func (h *Handler) authorizeIndexReadScope(w http.ResponseWriter, r *http.Request, principal AuthenticatedPrincipal, indexName string) bool {
	if principal.AllowsIndexRead(indexName) {
		return true
	}
	h.writeIndexScopeDenied(w, r, principal, "read", indexName)
	return false
}

func (h *Handler) authorizeSearchIndexScope(w http.ResponseWriter, r *http.Request, principal AuthenticatedPrincipal, query SearchQuery) bool {
	if strings.TrimSpace(query.Index) == "" {
		if principal.RequiresExplicitScopedIndex("search") {
			h.writeIndexScopeDenied(w, r, principal, "search", "")
			return false
		}
		return true
	}
	return h.authorizeIndexScope(w, r, principal, "search", query.Index)
}

func (h *Handler) authorizePluginScope(w http.ResponseWriter, r *http.Request, principal AuthenticatedPrincipal, action string, pluginType string, pluginCode string) bool {
	if principal.AllowsPlugin(action, pluginType, pluginCode) {
		return true
	}
	h.writePluginScopeDenied(w, r, principal, action, pluginType, pluginCode)
	return false
}

func (h *Handler) requireRBACBackend(w http.ResponseWriter) bool {
	if h.mysql == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "RBAC_BACKEND_UNAVAILABLE", "rbac backend is unavailable")
		return false
	}
	return true
}

func parseRBACPageQuery(r *http.Request) (int, int) {
	page := 1
	pageSize := 20
	if value := strings.TrimSpace(r.URL.Query().Get("page")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("page_size")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	return page, pageSize
}

func dataSourceStatusPermission(r *http.Request) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "datasource:update"
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var req struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "datasource:update"
	}
	switch normalizeStatus(req.Status) {
	case "active":
		return "datasource:start"
	case "disabled":
		return "datasource:stop"
	default:
		return "datasource:update"
	}
}

func (h *Handler) currentPrincipal(ctx context.Context) AuthenticatedPrincipal {
	principal, _ := principalFromContext(ctx)
	return principal
}

func (h *Handler) getCurrentUser(w http.ResponseWriter, r *http.Request) {
	principal := h.currentPrincipal(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":           principal.UserID,
			"username":     principal.Username,
			"display_name": principal.DisplayName,
		},
		"roles":       principal.Roles,
		"permissions": principal.PermissionCodes,
		"token": map[string]any{
			"id":     principal.TokenID,
			"name":   principal.TokenName,
			"source": principal.TokenSource,
		},
		"scopes": map[string]any{
			"indexes": principal.IndexScopes,
			"plugins": principal.PluginScopes,
		},
	})
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	page, pageSize := parseRBACPageQuery(r)
	items, pagination, err := h.mysql.ListUsers(r.Context(), mysqlstore.UserFilter{
		Page:     page,
		PageSize: pageSize,
		Query:    strings.TrimSpace(r.URL.Query().Get("query")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "list users failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": items, "pagination": pagination})
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	item, err := h.mysql.GetUser(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeError(w, http.StatusBadGateway, "get user failed")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	var req createUserAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid user request")
		return
	}
	principal := h.currentPrincipal(r.Context())
	item, err := h.mysql.CreateUser(r.Context(), mysqlstore.CreateUserRequest{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		Status:      req.Status,
		RoleIDs:     req.RoleIDs,
		ActorID:     principal.UserID,
	})
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "USER_CREATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	var req updateUserAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid user update request")
		return
	}
	principal := h.currentPrincipal(r.Context())
	item, err := h.mysql.UpdateUser(r.Context(), mysqlstore.UpdateUserRequest{
		ID:          strings.TrimSpace(r.PathValue("id")),
		DisplayName: req.DisplayName,
		Status:      req.Status,
		ActorID:     principal.UserID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "USER_UPDATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	userID := strings.TrimSpace(r.PathValue("id"))
	item, err := h.mysql.GetUser(r.Context(), userID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeError(w, http.StatusBadGateway, "get user before delete failed")
		return
	}
	if isProtectedAdminUser(item) {
		writeErrorCode(w, http.StatusBadRequest, "ADMIN_USER_DELETE_FORBIDDEN", "admin user cannot be deleted")
		return
	}
	if err := h.mysql.DeleteUser(r.Context(), userID); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "USER_DELETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func isProtectedAdminUser(item mysqlstore.AuthUserRecord) bool {
	username := strings.ToLower(strings.TrimSpace(item.Username))
	roleLabel := strings.ToLower(strings.TrimSpace(item.RoleLabel))
	return username == "admin" || roleLabel == "admin"
}

func (h *Handler) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	var req resetUserPasswordAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid password reset request")
		return
	}
	if err := h.mysql.SetUserPassword(r.Context(), strings.TrimSpace(r.PathValue("id")), req.Password); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "USER_PASSWORD_RESET_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true})
}

func (h *Handler) setUserRoles(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	var req setUserRolesAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid role binding request")
		return
	}
	principal := h.currentPrincipal(r.Context())
	if err := h.mysql.SetUserRoles(r.Context(), strings.TrimSpace(r.PathValue("id")), req.RoleIDs, principal.UserID); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "USER_ROLE_BIND_FAILED", err.Error())
		return
	}
	item, err := h.mysql.GetUser(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusBadGateway, "load user after role update failed")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	items, err := h.mysql.ListRoles(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "list roles failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": items})
}

func (h *Handler) getRole(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	item, err := h.mysql.GetRole(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "ROLE_NOT_FOUND", "role not found")
			return
		}
		writeError(w, http.StatusBadGateway, "get role failed")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	var req createRoleAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid role request")
		return
	}
	principal := h.currentPrincipal(r.Context())
	item, err := h.mysql.CreateRole(r.Context(), mysqlstore.CreateRoleRequest{
		RoleCode:        req.RoleCode,
		RoleName:        req.RoleName,
		Description:     req.Description,
		Status:          req.Status,
		PermissionCodes: req.PermissionCodes,
		IndexScopes:     req.IndexScopes,
		PluginScopes:    req.PluginScopes,
		ActorID:         principal.UserID,
	})
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "ROLE_CREATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	var req updateRoleAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid role update request")
		return
	}
	principal := h.currentPrincipal(r.Context())
	item, err := h.mysql.UpdateRole(r.Context(), mysqlstore.UpdateRoleRequest{
		ID:              strings.TrimSpace(r.PathValue("id")),
		RoleName:        req.RoleName,
		Description:     req.Description,
		Status:          req.Status,
		PermissionCodes: req.PermissionCodes,
		IndexScopes:     req.IndexScopes,
		PluginScopes:    req.PluginScopes,
		ActorID:         principal.UserID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "ROLE_NOT_FOUND", "role not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "ROLE_UPDATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	if err := h.mysql.DeleteRole(r.Context(), strings.TrimSpace(r.PathValue("id"))); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "ROLE_NOT_FOUND", "role not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "ROLE_DELETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handler) listPermissions(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	items, err := h.mysql.ListPermissions(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "list permissions failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": items})
}

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	principal := h.currentPrincipal(r.Context())
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		userID = principal.UserID
	}
	items, err := h.mysql.ListTokens(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "list tokens failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": items})
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	var req createTokenAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid token request")
		return
	}
	principal := h.currentPrincipal(r.Context())
	if strings.TrimSpace(req.UserID) == "" {
		req.UserID = principal.UserID
	}
	item, err := h.mysql.CreateScopedToken(r.Context(), mysqlstore.CreateScopedTokenRequest{
		UserID:          req.UserID,
		TokenName:       req.TokenName,
		ExpiresAt:       req.ExpiresAt,
		PermissionCodes: req.PermissionCodes,
		IndexScopes:     req.IndexScopes,
		PluginScopes:    req.PluginScopes,
		ActorID:         principal.UserID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "TOKEN_CREATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	principal := h.currentPrincipal(r.Context())
	if err := h.mysql.RevokeToken(r.Context(), strings.TrimSpace(r.PathValue("id")), principal.UserID); err != nil {
		if err == sql.ErrNoRows {
			writeErrorCode(w, http.StatusNotFound, "TOKEN_NOT_FOUND", "token not found")
			return
		}
		writeErrorCode(w, http.StatusBadRequest, "TOKEN_REVOKE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

func (h *Handler) listAuthAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !h.requireRBACBackend(w) {
		return
	}
	page, pageSize := parseRBACPageQuery(r)
	items, pagination, err := h.mysql.ListAuthAuditLogs(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, http.StatusBadGateway, "list audit logs failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit_logs": items, "pagination": pagination})
}
