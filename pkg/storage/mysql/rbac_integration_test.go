package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRBACMySQLIntegrationTokenScopeIntersection(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	analystRole := mustFindRoleByCode(t, ctx, client, "analyst")
	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("analyst"),
		DisplayName: "Analyst User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{analystRole.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if len(user.RoleCodes) == 0 {
		t.Fatalf("user roles should not be empty: %#v", user)
	}

	scopedToken, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "scoped-search",
		PermissionCodes: []string{"search:execute"},
	})
	if err != nil {
		t.Fatalf("create scoped token: %v", err)
	}

	record, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(scopedToken.Plaintext))
	if err != nil {
		t.Fatalf("resolve scoped principal: %v", err)
	}
	if !containsStringRBAC(record.EffectivePermissions, "search:execute") {
		t.Fatalf("effective permissions=%#v", record.EffectivePermissions)
	}
	if containsStringRBAC(record.EffectivePermissions, "datasource:create") {
		t.Fatalf("token scope should narrow user permissions, got %#v", record.EffectivePermissions)
	}
	if len(record.TokenPermissionCodes) != 1 || record.TokenPermissionCodes[0] != "search:execute" {
		t.Fatalf("token permissions=%#v", record.TokenPermissionCodes)
	}

	unscopedToken, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:    user.ID,
		TokenName: "unscoped-search",
	})
	if err != nil {
		t.Fatalf("create unscoped token: %v", err)
	}
	unscopedRecord, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(unscopedToken.Plaintext))
	if err != nil {
		t.Fatalf("resolve unscoped principal: %v", err)
	}
	if len(unscopedRecord.EffectivePermissions) <= len(record.EffectivePermissions) {
		t.Fatalf("unscoped token should inherit full user scope, scoped=%d unscoped=%d", len(record.EffectivePermissions), len(unscopedRecord.EffectivePermissions))
	}
	if !containsStringRBAC(unscopedRecord.EffectivePermissions, "search:saved_search") {
		t.Fatalf("expected inherited analyst permission in unscoped token, got %#v", unscopedRecord.EffectivePermissions)
	}
}

func TestRBACMySQLIntegrationRevokedTokenAndDisabledUserCannotResolvePrincipal(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	readonlyRole := mustFindRoleByCode(t, ctx, client, "readonly")
	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("readonly"),
		DisplayName: "Readonly User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{readonlyRole.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "readonly-token",
		PermissionCodes: []string{"search:execute"},
	})
	if err != nil {
		t.Fatalf("create scoped token: %v", err)
	}
	if err := client.RevokeToken(ctx, token.TokenID, "tester"); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	if _, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(token.Plaintext)); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after revoke, got %v", err)
	}

	activeToken, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "active-token",
		PermissionCodes: []string{"search:execute"},
	})
	if err != nil {
		t.Fatalf("create active token: %v", err)
	}
	if _, err := client.UpdateUser(ctx, UpdateUserRequest{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		Status:      "disabled",
		ActorID:     "tester",
	}); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if _, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(activeToken.Plaintext)); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for disabled user, got %v", err)
	}
}

func TestRBACMySQLIntegrationDeleteRoleIgnoresDeletedUserAssignments(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	role, err := client.CreateRole(ctx, CreateRoleRequest{
		RoleCode:        uniqueRBACIntegrationName("deleted-user-role"),
		RoleName:        "Deleted User Role",
		Status:          "active",
		PermissionCodes: []string{"search:execute"},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("deleted-user-role-user"),
		DisplayName: "Deleted User Role User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{role.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := client.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if err := client.DeleteRole(ctx, role.ID); err != nil {
		t.Fatalf("delete role after assigned user was deleted: %v", err)
	}
}

func TestRBACMySQLIntegrationTokenCannotExceedUserScope(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	readonlyRole := mustFindRoleByCode(t, ctx, client, "readonly")
	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("readonly-scope"),
		DisplayName: "Readonly Scope User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{readonlyRole.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "too-wide",
		PermissionCodes: []string{"datasource:create"},
	}); err == nil {
		t.Fatalf("expected token creation to reject permission outside user scope")
	}
}

func TestRBACMySQLIntegrationRoleIndexScopesResolveOnPrincipal(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	role, err := client.CreateRole(ctx, CreateRoleRequest{
		RoleCode:        uniqueRBACIntegrationName("index-scope-role"),
		RoleName:        "Index Scope Role",
		Description:     "index scoped analyst",
		Status:          "active",
		PermissionCodes: []string{"index:read", "index:manage", "search:execute", "search:fields", "search:timeline"},
		IndexScopes: IndexScopeMap{
			"read":   []string{"audit_*"},
			"manage": []string{"audit_p1_manual"},
			"search": []string{"audit_p1_manual"},
		},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("indexscope"),
		DisplayName: "Index Scope User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{role.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "index-scope-token",
		PermissionCodes: []string{"search:execute", "search:fields", "search:timeline"},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	record, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(token.Plaintext))
	if err != nil {
		t.Fatalf("resolve scoped principal: %v", err)
	}
	if !record.EffectiveIndexScopes["read"].Restricted || !reflect.DeepEqual(record.EffectiveIndexScopes["read"].Patterns, []string{"audit_*"}) {
		t.Fatalf("read scope = %#v", record.EffectiveIndexScopes["read"])
	}
	if !record.EffectiveIndexScopes["search"].Restricted || !reflect.DeepEqual(record.EffectiveIndexScopes["search"].Patterns, []string{"audit_p1_manual"}) {
		t.Fatalf("search scope = %#v", record.EffectiveIndexScopes["search"])
	}
	if !record.EffectiveIndexScopes["manage"].Restricted || !reflect.DeepEqual(record.EffectiveIndexScopes["manage"].Patterns, []string{"audit_p1_manual"}) {
		t.Fatalf("manage scope = %#v", record.EffectiveIndexScopes["manage"])
	}
}

func TestRBACMySQLIntegrationTokenIndexScopeIntersection(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	role, err := client.CreateRole(ctx, CreateRoleRequest{
		RoleCode:        uniqueRBACIntegrationName("token-index-scope-role"),
		RoleName:        "Token Index Scope Role",
		Description:     "token index scoped analyst",
		Status:          "active",
		PermissionCodes: []string{"search:execute"},
		IndexScopes: IndexScopeMap{
			"search": []string{"audit_*"},
		},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("tokenindexscope"),
		DisplayName: "Token Index Scope User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{role.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "token-index-scope",
		PermissionCodes: []string{"search:execute"},
		IndexScopes: IndexScopeMap{
			"search": []string{"audit_p1_manual"},
		},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	record, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(token.Plaintext))
	if err != nil {
		t.Fatalf("resolve scoped principal: %v", err)
	}
	if !record.EffectiveIndexScopes["search"].Restricted || !reflect.DeepEqual(record.EffectiveIndexScopes["search"].Patterns, []string{"audit_p1_manual"}) {
		t.Fatalf("effective search scope = %#v", record.EffectiveIndexScopes["search"])
	}

	if _, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "token-index-scope-wide",
		PermissionCodes: []string{"search:execute"},
		IndexScopes: IndexScopeMap{
			"search": []string{"json_*"},
		},
	}); err == nil {
		t.Fatalf("expected token creation to reject index scope outside user scope")
	}
}

func TestRBACMySQLIntegrationRolePluginScopesResolveOnPrincipal(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	role, err := client.CreateRole(ctx, CreateRoleRequest{
		RoleCode:        uniqueRBACIntegrationName("plugin-scope-role"),
		RoleName:        "Plugin Scope Role",
		Description:     "plugin scoped config admin",
		Status:          "active",
		PermissionCodes: []string{"datasource:read", "parse_rule:read", "search:execute"},
		PluginScopes: PluginScopeMap{
			"use": {
				{PluginType: "input", PluginCode: "syslog"},
				{PluginType: "parser", PluginCode: "regex"},
			},
			"manage": {
				{PluginType: "parser", PluginCode: "*"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("pluginscope"),
		DisplayName: "Plugin Scope User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{role.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "plugin-scope-token",
		PermissionCodes: []string{"search:execute"},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	record, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(token.Plaintext))
	if err != nil {
		t.Fatalf("resolve scoped principal: %v", err)
	}
	if !reflect.DeepEqual(record.EffectivePluginScopes["use"], []PluginScopeBinding{
		{PluginType: "input", PluginCode: "syslog"},
		{PluginType: "parser", PluginCode: "regex"},
	}) {
		t.Fatalf("use scope = %#v", record.EffectivePluginScopes["use"])
	}
	if !reflect.DeepEqual(record.EffectivePluginScopes["manage"], []PluginScopeBinding{
		{PluginType: "parser", PluginCode: "*"},
	}) {
		t.Fatalf("manage scope = %#v", record.EffectivePluginScopes["manage"])
	}
}

func TestRBACMySQLIntegrationTokenPluginScopeIntersection(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	role, err := client.CreateRole(ctx, CreateRoleRequest{
		RoleCode:        uniqueRBACIntegrationName("token-plugin-scope-role"),
		RoleName:        "Token Plugin Scope Role",
		Description:     "token plugin scoped config admin",
		Status:          "active",
		PermissionCodes: []string{"search:execute"},
		PluginScopes: PluginScopeMap{
			"use": {
				{PluginType: "search_command", PluginCode: "*"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("tokenpluginscope"),
		DisplayName: "Token Plugin Scope User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{role.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "token-plugin-scope",
		PermissionCodes: []string{"search:execute"},
		PluginScopes: PluginScopeMap{
			"use": {
				{PluginType: "search_command", PluginCode: "stats"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	record, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(token.Plaintext))
	if err != nil {
		t.Fatalf("resolve scoped principal: %v", err)
	}
	if !reflect.DeepEqual(record.EffectivePluginScopes["use"], []PluginScopeBinding{
		{PluginType: "search_command", PluginCode: "stats"},
	}) {
		t.Fatalf("effective use scope = %#v", record.EffectivePluginScopes["use"])
	}

	if _, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:          user.ID,
		TokenName:       "token-plugin-too-wide",
		PermissionCodes: []string{"search:execute"},
		PluginScopes: PluginScopeMap{
			"use": {
				{PluginType: "parser", PluginCode: "regex"},
			},
		},
	}); err == nil {
		t.Fatalf("expected plugin scope creation to reject scope outside user scope")
	}
}

func TestRBACMySQLIntegrationRoleChangeImmediatelyAffectsEffectivePermissions(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	readonlyRole := mustFindRoleByCode(t, ctx, client, "readonly")
	configAdminRole := mustFindRoleByCode(t, ctx, client, "config_admin")
	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username:    uniqueRBACIntegrationName("role-change"),
		DisplayName: "Role Change User",
		Password:    "ChangeMe_123",
		Status:      "active",
		RoleIDs:     []string{readonlyRole.ID},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := client.CreateScopedToken(ctx, CreateScopedTokenRequest{
		UserID:    user.ID,
		TokenName: "unscoped-role-change",
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	before, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(token.Plaintext))
	if err != nil {
		t.Fatalf("resolve before role change: %v", err)
	}
	if containsStringRBAC(before.EffectivePermissions, "datasource:create") {
		t.Fatalf("readonly user should not have datasource:create before role change: %#v", before.EffectivePermissions)
	}

	if err := client.SetUserRoles(ctx, user.ID, []string{configAdminRole.ID}, "tester"); err != nil {
		t.Fatalf("set user roles: %v", err)
	}

	after, err := client.ResolvePrincipalByTokenHash(ctx, hashRBACSecret(token.Plaintext))
	if err != nil {
		t.Fatalf("resolve after role change: %v", err)
	}
	if !containsStringRBAC(after.EffectivePermissions, "datasource:create") {
		t.Fatalf("config_admin permission should take effect immediately, got %#v", after.EffectivePermissions)
	}
}

func TestPluginStatusMySQLIntegrationIsIdempotent(t *testing.T) {
	client := openRBACIntegrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	code := strings.ReplaceAll(uniqueRBACIntegrationName("table-idempotent"), "_", "-")
	record := PluginRecord{
		PluginCode:       code,
		PluginType:       "search_command",
		PluginVersion:    "1.0.0",
		Name:             "Table Idempotent",
		Runtime:          "executable_search_command",
		Entrypoint:       "bin/table_command.py",
		Status:           "disabled",
		Checksum:         "sha256:test",
		ConfigSchema:     json.RawMessage(`{"type":"object","properties":{}}`),
		UISchema:         json.RawMessage(`{}`),
		InputSchema:      json.RawMessage(`{}`),
		OutputSchema:     json.RawMessage(`{}`),
		PermissionSchema: json.RawMessage(`{}`),
		RuntimeConfig:    json.RawMessage(`{"interpreter":"python3"}`),
		PackageBytes:     []byte("package"),
	}
	if err := client.UpsertPluginRecord(ctx, record); err != nil {
		t.Fatalf("upsert plugin record: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = client.SetPluginStatus(cleanupCtx, "search_command", code, "disabled")
		_ = client.DeletePlugin(cleanupCtx, "search_command", code)
	})

	enabled, err := client.SetPluginStatus(ctx, "search_command", code, "enabled")
	if err != nil {
		t.Fatalf("first enable plugin: %v", err)
	}
	if enabled.Status != "enabled" {
		t.Fatalf("first enable status = %q, want enabled", enabled.Status)
	}

	enabledAgain, err := client.SetPluginStatus(ctx, "search_command", code, "enabled")
	if err != nil {
		t.Fatalf("second enable should be idempotent, got %v", err)
	}
	if enabledAgain.Status != "enabled" {
		t.Fatalf("second enable status = %q, want enabled", enabledAgain.Status)
	}
}

func openRBACIntegrationClient(t *testing.T) *Client {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("XDP_TEST_MYSQL_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("XDP_MYSQL_DSN"))
	}
	if dsn == "" {
		dsn = "xdp:xdp@tcp(127.0.0.1:3306)/xdp?parseTime=true&multiStatements=true"
	}
	client, err := Open(Config{DSN: dsn})
	if err != nil {
		t.Skipf("open mysql skipped: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		_ = client.Close()
		t.Skipf("mysql integration skipped: %v", err)
	}
	if err := client.Migrate(ctx); err != nil {
		_ = client.Close()
		t.Fatalf("migrate mysql: %v", err)
	}
	cleanupRBACIntegrationTables(t, ctx, client)
	if err := client.EnsureRBACSeeds(ctx); err != nil {
		_ = client.Close()
		t.Fatalf("seed rbac: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cleanupRBACIntegrationTables(t, cleanupCtx, client)
		_ = client.Close()
	})
	return client
}

func cleanupRBACIntegrationTables(t *testing.T, ctx context.Context, client *Client) {
	t.Helper()
	statements := []string{
		"SET FOREIGN_KEY_CHECKS = 0",
		"TRUNCATE TABLE auth_token_plugin_scopes",
		"TRUNCATE TABLE auth_token_index_scopes",
		"TRUNCATE TABLE auth_token_permissions",
		"TRUNCATE TABLE auth_role_plugin_scopes",
		"TRUNCATE TABLE auth_role_index_scopes",
		"TRUNCATE TABLE auth_user_roles",
		"TRUNCATE TABLE auth_role_permissions",
		"TRUNCATE TABLE auth_tokens",
		"TRUNCATE TABLE auth_roles",
		"TRUNCATE TABLE auth_permissions",
		"TRUNCATE TABLE auth_audit_logs",
		"TRUNCATE TABLE auth_users",
		"SET FOREIGN_KEY_CHECKS = 1",
	}
	for _, statement := range statements {
		if _, err := client.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("cleanup %q: %v", statement, err)
		}
	}
}

func mustFindRoleByCode(t *testing.T, ctx context.Context, client *Client, roleCode string) AuthRoleRecord {
	t.Helper()
	items, err := client.ListRoles(ctx)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	for _, item := range items {
		if item.RoleCode == roleCode {
			return item
		}
	}
	t.Fatalf("role %s not found", roleCode)
	return AuthRoleRecord{}
}

func uniqueRBACIntegrationName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func containsStringRBAC(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
