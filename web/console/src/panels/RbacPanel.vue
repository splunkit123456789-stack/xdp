<template>
  <section data-testid="rbac-page" class="tab-panel">
    <div class="panel-header"><h2><span class="page-icon page-icon-rbac">RB</span>用户与权限</h2><span class="badge">用户 / 角色</span></div>
    <p v-if="rbacNotice" data-testid="rbac-notice" class="status-line">{{ rbacNotice }}</p>
    <p v-if="rbacError" data-testid="rbac-error" class="field-error form-error">{{ rbacError }}</p>
    <div v-if="hasPermission('rbac:manage')" class="plugin-type-tabs rbac-tabs" role="tablist" aria-label="用户与角色">
      <button data-testid="rbac-tab-users" :class="{ active: currentRBACTab === 'users' }" type="button" role="tab" :aria-selected="currentRBACTab === 'users'" @click="currentRBACTab = 'users'">
        <span>用户</span>
        <small>{{ rbacUserPagination.total || rbacUsers.length }}</small>
      </button>
      <button data-testid="rbac-tab-roles" :class="{ active: currentRBACTab === 'roles' }" type="button" role="tab" :aria-selected="currentRBACTab === 'roles'" @click="currentRBACTab = 'roles'">
        <span>角色</span>
        <small>{{ rbacRoles.length }}</small>
      </button>
    </div>
    <div class="content-grid rbac-grid">
      <article v-if="hasPermission('rbac:manage') && currentRBACTab === 'users'" data-testid="rbac-users-panel" class="card">
        <div class="card-head">
          <span>用户管理</span>
          <div class="rbac-card-actions">
            <span class="status-line">{{ rbacUserPagination.total || rbacUsers.length }} 个用户</span>
            <button data-testid="show-user-modal" class="btn" type="button" @click="openCreateUserModal">新建用户</button>
          </div>
        </div>
        <div class="table-wrap">
          <table data-testid="rbac-users-table">
            <thead><tr><th>用户名</th><th>显示名</th><th>状态</th><th>角色</th><th>最近登录时间</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-if="!rbacUsers.length"><td colspan="6">暂无用户</td></tr>
              <tr v-for="user in rbacUsers" :key="user.id || user.username">
                <td><code>{{ user.username }}</code></td>
                <td>{{ user.display_name || user.displayName || "-" }}</td>
                <td><span class="status-pill" :class="user.status === 'active' ? 'runtime-running' : 'runtime-stopped'">{{ user.status }}</span></td>
                <td>{{ roleNames(user.roles) }}</td>
                <td>{{ formatFullTime(user.last_login_at || user.lastLoginAt) }}</td>
                <td>
                  <div class="row-actions">
                    <button :data-testid="`edit-user-${user.id}`" class="link-btn" type="button" @click="openEditUserModal(user)">修改</button>
                    <button :data-testid="`toggle-user-${user.id}`" class="link-btn" type="button" @click="toggleRBACUserStatus(user)">{{ user.status === "active" ? "禁用" : "启用" }}</button>
                    <button :data-testid="`reset-password-${user.id}`" class="link-btn" type="button" @click="resetRBACUserPassword(user)">重置密码</button>
                    <button v-if="!isProtectedAdminUser(user)" :data-testid="`delete-user-${user.id}`" class="link-btn delete" type="button" @click="deleteRBACUser(user)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </article>

      <article v-if="hasPermission('rbac:manage') && currentRBACTab === 'roles'" data-testid="rbac-roles-panel" class="card">
        <div class="card-head">
          <span>角色管理</span>
          <div class="rbac-card-actions">
            <span class="status-line">{{ rbacRoles.length }} 个角色</span>
            <button data-testid="show-role-modal" class="btn" type="button" @click="openCreateRoleModal">新建角色</button>
          </div>
        </div>
        <div class="table-wrap">
          <table data-testid="rbac-roles-table">
            <thead><tr><th>角色</th><th>状态</th><th>权限</th><th>索引</th><th>Plugin Scope</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-if="!rbacRoles.length"><td colspan="6">暂无角色</td></tr>
              <tr v-for="role in rbacRoles" :key="role.id || role.role_code">
                <td><code>{{ role.role_code }}</code><div>{{ role.role_name }}</div></td>
                <td><span class="status-pill" :class="role.status === 'active' ? 'runtime-running' : 'runtime-stopped'">{{ role.status }}</span></td>
                <td><code class="multiline-code">{{ formatRolePermissionSummary(role.permission_codes) }}</code></td>
                <td><code class="multiline-code">{{ formatRoleIndexScopes(role) }}</code></td>
                <td><code class="multiline-code">{{ formatPluginScopes(role.plugin_scopes) }}</code></td>
                <td>
                  <div class="row-actions">
                    <button :data-testid="`edit-role-${role.id}`" class="link-btn" type="button" @click="openEditRoleModal(role)">修改</button>
                    <button v-if="!role.builtin" :data-testid="`delete-role-${role.id}`" class="link-btn delete" type="button" @click="deleteRBACRole(role)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </article>
    </div>

    <article v-if="showUserModal" class="card config-drawer rbac-drawer" data-testid="user-modal" role="dialog" aria-modal="true">
      <form class="rbac-modal" data-testid="create-user" @submit.prevent="submitUserModal">
        <header class="rbac-modal-head">
          <div>
            <span class="form-hint">用户</span>
            <h3>{{ editingRBACUserId ? "修改用户" : "新建用户" }}</h3>
          </div>
          <button data-testid="close-user-modal" class="rbac-modal-close" type="button" aria-label="关闭" @click="closeUserModal">×</button>
        </header>
        <div class="rbac-modal-body">
          <div class="two">
            <label>账号<input v-model="rbacUserForm.username" data-testid="user-username" class="field" required placeholder="请输入账号" /></label>
            <label>全称<input v-model="rbacUserForm.displayName" data-testid="user-display-name" class="field" required placeholder="请输入全称" /></label>
          </div>
          <div class="two">
            <label>{{ editingRBACUserId ? "新密码" : "设置密码" }}<input v-model="rbacUserForm.password" data-testid="user-password" class="field" type="password" :required="!editingRBACUserId" :placeholder="editingRBACUserId ? '留空则不修改密码' : '请输入密码'" /></label>
            <label>确认密码<input v-model="rbacUserForm.confirmPassword" data-testid="user-confirm-password" class="field" type="password" :required="!editingRBACUserId || Boolean(rbacUserForm.password)" placeholder="请再次输入密码" /></label>
          </div>
          <p class="form-hint">密码建议至少 8 位，并包含大小写字母、数字或特殊字符。</p>
          <div class="two">
            <label>状态<select v-model="rbacUserForm.status" data-testid="user-status" class="select" required><option value="active">active</option><option value="disabled">disabled</option></select></label>
            <div class="checkbox-panel rbac-option-panel">
              <label class="check-row">
                <input v-model="rbacUserForm.createRoleForUser" data-testid="user-create-role" type="checkbox" />
                <span>为此用户新建角色</span>
              </label>
              <label class="check-row">
                <input v-model="rbacUserForm.forcePasswordChange" data-testid="user-force-password-change" type="checkbox" />
                <span>首次登录需要更改密码</span>
              </label>
            </div>
          </div>
          <section class="rbac-transfer" data-testid="user-role-transfer">
            <div class="rbac-transfer-col">
              <div class="rbac-transfer-head"><strong>可分配角色</strong><button class="link-btn" type="button" @click="addAllUserRoles">全部添加</button></div>
              <button v-for="role in availableUserRoles" :key="roleId(role)" :data-testid="`user-role-option-${permissionTestId(roleId(role))}`" class="rbac-list-item" type="button" @click="addUserRole(role)">
                <span>{{ role.role_name || role.role_code }}</span>
                <code>{{ role.role_code }}</code>
              </button>
              <p v-if="!availableUserRoles.length" class="status-line">暂无可分配角色</p>
            </div>
            <div class="rbac-transfer-col">
              <div class="rbac-transfer-head"><strong>已分配角色</strong><button class="link-btn" type="button" @click="removeAllUserRoles">全部删除</button></div>
              <button v-for="role in selectedUserRoles" :key="roleId(role)" :data-testid="`user-role-selected-${permissionTestId(roleId(role))}`" class="rbac-list-item selected" type="button" @click="removeUserRole(role)">
                <span>{{ role.role_name || role.role_code }}</span>
                <code>{{ role.role_code }}</code>
              </button>
              <p v-if="!selectedUserRoles.length" class="status-line">暂未分配角色</p>
            </div>
          </section>
          <p v-if="rbacUserError" data-testid="user-form-error" class="field-error form-error">{{ rbacUserError }}</p>
          <div class="actions rbac-modal-footer">
            <button data-testid="cancel-user-modal" class="btn ghost" type="button" @click="closeUserModal">取消</button>
            <button class="btn" type="submit">保存</button>
          </div>
        </div>
      </form>
    </article>

    <article v-if="showRoleModal" class="card config-drawer rbac-drawer rbac-role-drawer" data-testid="role-modal" role="dialog" aria-modal="true">
      <form class="rbac-modal rbac-role-modal" data-testid="create-role" @submit.prevent="submitRoleModal">
        <header class="rbac-modal-head">
          <div>
            <span class="form-hint">角色</span>
            <h3>{{ editingRBACRoleId ? "修改角色" : "新建角色" }}</h3>
          </div>
          <button data-testid="close-role-modal" class="rbac-modal-close" type="button" aria-label="关闭" @click="closeRoleModal">×</button>
        </header>
        <div class="rbac-modal-body">
          <div class="two">
            <label>角色编码<input v-model="rbacRoleForm.roleCode" data-testid="role-code" class="field" required placeholder="search_limited" /></label>
            <label>角色名称<input v-model="rbacRoleForm.roleName" data-testid="role-name" class="field" required placeholder="受限搜索" /></label>
          </div>
          <div class="two">
            <label>状态<select v-model="rbacRoleForm.status" data-testid="role-status" class="select" required><option value="active">active</option><option value="disabled">disabled</option></select></label>
            <label>描述<input v-model="rbacRoleForm.description" data-testid="role-description" class="field" placeholder="角色说明" /></label>
          </div>
          <div class="rbac-modal-tabs" role="tablist" aria-label="角色配置">
            <button data-testid="role-modal-tab-inherit" :class="{ active: currentRBACRoleModalTab === 'inherit' }" type="button" role="tab" @click="currentRBACRoleModalTab = 'inherit'"><span class="tab-step">1</span>继承</button>
            <button data-testid="role-modal-tab-menu" :class="{ active: currentRBACRoleModalTab === 'menu' }" type="button" role="tab" @click="currentRBACRoleModalTab = 'menu'"><span class="tab-step">2</span>菜单</button>
            <button data-testid="role-modal-tab-plugin" :class="{ active: currentRBACRoleModalTab === 'plugin' }" type="button" role="tab" @click="currentRBACRoleModalTab = 'plugin'"><span class="tab-step">3</span>插件</button>
            <button data-testid="role-modal-tab-index" :class="{ active: currentRBACRoleModalTab === 'index' }" type="button" role="tab" @click="currentRBACRoleModalTab = 'index'"><span class="tab-step">4</span>索引</button>
          </div>
          <div class="rbac-role-tab-frame" data-testid="role-tab-frame">
            <section v-if="currentRBACRoleModalTab === 'inherit'" class="checkbox-panel rbac-role-tab-panel" data-testid="role-inheritance">
              <span class="form-hint">继承角色用于产品编排展示，P2 保存显式菜单、插件和索引授权。</span>
              <label v-for="role in rbacRoles" :key="roleId(role)" class="check-row">
                <input v-model="rbacRoleForm.inheritedRoleIds" :data-testid="`role-inherit-${permissionTestId(roleId(role))}`" type="checkbox" :value="roleId(role)" />
                <span>{{ role.role_name || role.role_code }}</span>
              </label>
              <p v-if="!rbacRoles.length" class="status-line">暂无可继承角色</p>
            </section>
            <section v-if="currentRBACRoleModalTab === 'menu'" class="checkbox-panel rbac-role-tab-panel" data-testid="role-permissions">
              <label v-for="permission in menuRBACPermissions" :key="permission.permission_code" class="check-row">
                <input v-model="rbacRoleForm.permissionCodes" :data-testid="`role-permission-${permissionTestId(permission.permission_code)}`" type="checkbox" :value="permission.permission_code" />
                <span><strong>{{ menuPermissionLabel(permission.permission_code) }}</strong></span>
              </label>
              <p v-if="!menuRBACPermissions.length" class="status-line">暂无可分配菜单权限</p>
            </section>
            <section v-if="currentRBACRoleModalTab === 'plugin'" class="rbac-modal-section rbac-role-tab-panel">
              <label>外部插件授权<textarea v-model="rbacRoleForm.pluginScopesText" data-testid="role-plugin-scopes" class="props-editor" placeholder="每行一个配置，例如 use:search_command/table 或 manage:input/kafka"></textarea></label>
              <p class="form-hint">插件维度只面向外部导入插件，只保留 use / manage 两类授权；内置 syslog、regex、stats 不需要配置。</p>
            </section>
            <section v-if="currentRBACRoleModalTab === 'index'" class="rbac-modal-section rbac-role-tab-panel">
              <div class="checkbox-panel" data-testid="role-index-list">
                <label v-for="item in roleIndexOptions" :key="item.name" class="check-row">
                  <input :checked="isRoleSearchIndexSelected(item.name)" :data-testid="`role-index-item-${permissionTestId(item.name)}`" type="checkbox" @change="toggleRoleSearchIndex(item.name)" />
                  <span><strong>{{ item.name }}</strong></span>
                </label>
                <p v-if="!roleIndexOptions.length" class="status-line">暂无可配置索引</p>
              </div>
            </section>
          </div>
          <p v-if="rbacRoleError" data-testid="role-form-error" class="field-error form-error">{{ rbacRoleError }}</p>
          <div class="actions rbac-modal-footer">
            <button data-testid="cancel-role-modal" class="btn ghost" type="button" @click="closeRoleModal">取消</button>
            <button class="btn" type="submit">保存</button>
          </div>
        </div>
      </form>
    </article>
  </section>
</template>

<script>
import { panelPropNames } from "./panel-props.js";

export default {
  name: "RbacPanel",
  props: panelPropNames,
  data() {
    return {
      currentRBACTab: "users",
      currentRBACRoleModalTab: "inherit",
      showUserModal: false,
      showRoleModal: false
    };
  },
  computed: {
    selectedUserRoles() {
      const selected = new Set((this.rbacUserForm.roleIds || []).map((item) => String(item)));
      return (this.rbacRoles || []).filter((role) => selected.has(String(this.roleId(role))));
    },
    availableUserRoles() {
      const selected = new Set((this.rbacUserForm.roleIds || []).map((item) => String(item)));
      return (this.rbacRoles || []).filter((role) => !selected.has(String(this.roleId(role))));
    },
    menuRBACPermissions() {
      const menuCodes = new Set(["datasource:read", "parse_rule:read", "index:read", "search:execute", "rbac:manage"]);
      return (this.assignableRBACPermissions || []).filter((permission) => menuCodes.has(permission.permission_code));
    },
    roleIndexOptions() {
      const items = Array.isArray(this.businessIndexes) && this.businessIndexes.length ? this.businessIndexes : (this.indexes || []);
      const seen = new Set();
      return items.map((item) => ({
        name: String(item.index_name || item.name || item.index || "").trim()
      })).filter((item) => {
        if (!item.name || item.name.startsWith("_") || seen.has(item.name)) return false;
        seen.add(item.name);
        return true;
      });
    }
  },
  mounted() {
    document.addEventListener("pointerdown", this.handleOutsidePointerDown, true);
  },
  beforeUnmount() {
    document.removeEventListener("pointerdown", this.handleOutsidePointerDown, true);
  },
  methods: {
    roleId(role) {
      return role?.id || role?.role_id || role?.role_code || "";
    },
    isProtectedAdminUser(user) {
      const username = String(user?.username || "").trim().toLowerCase();
      const roleLabel = String(user?.role_label || user?.roleLabel || "").trim().toLowerCase();
      return username === "admin" || roleLabel === "admin";
    },
    menuPermissionLabel(code) {
      return {
        "datasource:read": "采集配置",
        "parse_rule:read": "解析配置",
        "index:read": "索引配置",
        "search:execute": "搜索页",
        "rbac:manage": "用户与权限"
      }[code] || code;
    },
    formatRolePermissionSummary(codes = []) {
      const values = Array.isArray(codes) ? codes : [];
      const menu = values.filter((code) => this.menuRBACPermissions.some((permission) => permission.permission_code === code)).map((code) => this.menuPermissionLabel(code));
      const lines = [];
      if (menu.length) lines.push(`菜单：${menu.join("、")}`);
      return lines.length ? lines.join("\n") : "-";
    },
    formatRoleIndexScopes(role) {
      const roleCode = String(role?.role_code || role?.roleCode || "").trim();
      if (roleCode === "platform_admin") return "*";
      const values = this.roleSearchIndexValues(role?.index_scopes || role?.indexScopes);
      return values.length ? values.join("\n") : "-";
    },
    roleSearchIndexValues(scopes = {}) {
      const searchValues = Array.isArray(scopes?.search) ? scopes.search : [];
      return Array.from(new Set(searchValues.map((item) => String(item || "").trim()).filter(Boolean)));
    },
    selectedRoleSearchIndexes() {
      return String(this.rbacRoleForm.indexScopesText || "").split("\n").map((line) => line.trim()).filter(Boolean).map((line) => {
        const index = line.indexOf(":");
        return index >= 0 ? line.slice(index + 1).trim() : line;
      }).filter(Boolean);
    },
    isRoleSearchIndexSelected(indexName) {
      return this.selectedRoleSearchIndexes().includes(String(indexName || "").trim());
    },
    toggleRoleSearchIndex(indexName) {
      const name = String(indexName || "").trim();
      if (!name) return;
      const selected = new Set(this.selectedRoleSearchIndexes());
      if (selected.has(name)) {
        selected.delete(name);
      } else {
        selected.add(name);
      }
      this.rbacRoleForm.indexScopesText = Array.from(selected).map((item) => `search:${item}`).join("\n");
    },
    handleOutsidePointerDown(event) {
      if (!this.showUserModal && !this.showRoleModal) return;
      const target = event.target;
      if (target?.closest?.(".config-drawer")) return;
      if (this.showUserModal) this.closeUserModal();
      if (this.showRoleModal) this.closeRoleModal();
    },
    openCreateUserModal() {
      this.resetRBACUserForm();
      this.showUserModal = true;
    },
    openEditUserModal(user) {
      this.editRBACUser(user);
      this.showUserModal = true;
    },
    closeUserModal() {
      this.showUserModal = false;
      this.resetRBACUserForm();
    },
    async submitUserModal() {
      await this.saveRBACUser();
      await this.$nextTick();
      if (!this.rbacUserError) this.showUserModal = false;
    },
    addUserRole(role) {
      const id = this.roleId(role);
      if (!id || (this.rbacUserForm.roleIds || []).map(String).includes(String(id))) return;
      this.rbacUserForm.roleIds = [...(this.rbacUserForm.roleIds || []), id];
    },
    removeUserRole(role) {
      const id = this.roleId(role);
      this.rbacUserForm.roleIds = (this.rbacUserForm.roleIds || []).filter((item) => String(item) !== String(id));
    },
    addAllUserRoles() {
      this.rbacUserForm.roleIds = Array.from(new Set([...(this.rbacUserForm.roleIds || []), ...(this.rbacRoles || []).map((role) => this.roleId(role)).filter(Boolean)]));
    },
    removeAllUserRoles() {
      this.rbacUserForm.roleIds = [];
    },
    openCreateRoleModal() {
      this.resetRBACRoleForm();
      this.currentRBACRoleModalTab = "inherit";
      this.showRoleModal = true;
    },
    openEditRoleModal(role) {
      this.editRBACRole(role);
      this.currentRBACRoleModalTab = "inherit";
      this.showRoleModal = true;
    },
    closeRoleModal() {
      this.showRoleModal = false;
      this.resetRBACRoleForm();
    },
    async submitRoleModal() {
      await this.saveRBACRole();
      await this.$nextTick();
      if (!this.rbacRoleError) this.showRoleModal = false;
    }
  }
};
</script>
