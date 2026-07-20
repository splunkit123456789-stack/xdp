package mysql

import (
	"os"
	"strings"
	"testing"
)

func TestRBACRuntimeSchemaTablesExist(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS auth_roles",
		"CREATE TABLE IF NOT EXISTS auth_permissions",
		"CREATE TABLE IF NOT EXISTS auth_role_permissions",
		"CREATE TABLE IF NOT EXISTS auth_user_roles",
		"CREATE TABLE IF NOT EXISTS auth_token_permissions",
		"CREATE TABLE IF NOT EXISTS auth_role_index_scopes",
		"CREATE TABLE IF NOT EXISTS auth_token_index_scopes",
		"CREATE TABLE IF NOT EXISTS auth_role_plugin_scopes",
		"CREATE TABLE IF NOT EXISTS auth_token_plugin_scopes",
	}
	for _, want := range required {
		if !strings.Contains(schema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRBACRuntimeSchemaContainsUniqueKeys(t *testing.T) {
	required := []string{
		"UNIQUE KEY uk_auth_roles_code (role_code)",
		"UNIQUE KEY uk_auth_permissions_code (permission_code)",
		"UNIQUE KEY uk_auth_role_permissions (role_id, permission_id)",
		"UNIQUE KEY uk_auth_user_roles (user_id, role_id)",
		"UNIQUE KEY uk_auth_token_permissions (token_id, permission_id)",
		"UNIQUE KEY uk_auth_role_index_scopes (role_id, scope_action, index_pattern)",
		"UNIQUE KEY uk_auth_token_index_scopes (token_id, scope_action, index_pattern)",
		"UNIQUE KEY uk_auth_role_plugin_scopes (role_id, scope_action, plugin_type, plugin_code)",
		"UNIQUE KEY uk_auth_token_plugin_scopes (token_id, scope_action, plugin_type, plugin_code)",
	}
	for _, want := range required {
		if !strings.Contains(schema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRBACMigrationIncludesTables(t *testing.T) {
	data, err := os.ReadFile("../../../migrations/mysql/000001_core_metadata.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	migration := string(data)
	required := []string{
		"CREATE TABLE auth_roles",
		"CREATE TABLE auth_permissions",
		"CREATE TABLE auth_role_permissions",
		"CREATE TABLE auth_user_roles",
		"CREATE TABLE auth_token_permissions",
		"CREATE TABLE auth_role_index_scopes",
		"CREATE TABLE auth_token_index_scopes",
		"CREATE TABLE auth_role_plugin_scopes",
		"CREATE TABLE auth_token_plugin_scopes",
	}
	for _, want := range required {
		if !strings.Contains(migration, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestRBACRuntimeSchemaContainsNoWorkspaceArtifacts(t *testing.T) {
	forbidden := "ten" + "ant"
	if strings.Contains(strings.ToLower(schema), forbidden) {
		t.Fatalf("rbac schema must not contain workspace isolation artifacts")
	}
}
