<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  AlertCircle,
  Check,
  ChevronLeft,
  ChevronRight,
  Edit3,
  FileKey,
  KeyRound,
  LoaderCircle,
  LockKeyhole,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Trash2,
  UserCog,
  Users,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

const { t } = useI18n();
const route = useRoute();
const auth = useAuthStore();
const canManage = computed(() => auth.hasPermission("system.manage"));

type Tab = "admins" | "roles" | "audit";

interface Permission {
  id: string;
  code: string;
  name: string;
  module: string;
  description?: string;
}

interface RoleSummary {
  id: string;
  code: string;
  name: string;
  description: string;
  system: boolean;
  permission_ids: string[];
  admin_count: number;
}

interface AdminRoleSummary {
  id: string;
  code: string;
  name: string;
}

interface AdminSummary {
  id: string;
  username: string;
  name: string;
  status: string;
  last_login_at?: string | null;
  created_at: string;
  session_version?: number;
  totp_enabled: boolean;
  roles: AdminRoleSummary[];
  role_ids: string[];
}

interface AuditLog {
  id: string;
  admin_id?: string | null;
  admin_name?: string;
  admin_username?: string;
  action: string;
  resource: string;
  resource_id: string;
  ip: string;
  detail: string;
  created_at: string;
}

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface AdminForm {
  username: string;
  name: string;
  password: string;
  status: "active" | "disabled";
  role_ids: string[];
  reason: string;
}

interface RoleForm {
  code: string;
  name: string;
  description: string;
  permission_ids: string[];
  reason: string;
}

const activeTab = ref<Tab>("admins");

const admins = ref<AdminSummary[]>([]);
const roles = ref<RoleSummary[]>([]);
const permissions = ref<Permission[]>([]);
const overviewLoading = ref(false);
const loadError = ref("");
const notice = ref("");
const currentAdminID = ref("");

const auditLogs = ref<AuditLog[]>([]);
const auditTotal = ref(0);
const auditPage = ref(1);
const auditPageSize = ref(20);
const auditLoading = ref(false);
const auditSearch = ref("");
const auditAction = ref("");
const auditResource = ref("");
const auditAdminID = ref("");
const auditDateFrom = ref("");
const auditDateTo = ref("");

const adminModal = ref(false);
const editingAdmin = ref<AdminSummary | null>(null);
const adminSaving = ref(false);
const adminError = ref("");
const adminForm = ref<AdminForm>({
  username: "",
  name: "",
  password: "",
  status: "active",
  role_ids: [],
  reason: "",
});

const roleModal = ref(false);
const editingRole = ref<RoleSummary | null>(null);
const roleSaving = ref(false);
const roleError = ref("");
const roleForm = ref<RoleForm>({
  code: "",
  name: "",
  description: "",
  permission_ids: [],
  reason: "",
});

const passwordModal = ref(false);
const passwordAdmin = ref<AdminSummary | null>(null);
const currentPassword = ref("");
const newPassword = ref("");
const passwordReason = ref("");
const passwordSaving = ref(false);
const passwordError = ref("");

const deleteModal = ref(false);
const deletingRole = ref<RoleSummary | null>(null);
const deleteReason = ref("");
const deleteSaving = ref(false);
const deleteError = ref("");

const activeAdmins = computed(
  () => admins.value.filter((admin) => admin.status === "active").length,
);
const protectedAdmins = computed(
  () =>
    admins.value.filter((admin) =>
      admin.roles.some((role) => role.code === "super_admin"),
    ).length,
);
const customRoles = computed(
  () => roles.value.filter((role) => !role.system).length,
);
const permissionGroups = computed(() => {
  const result = new Map<string, Permission[]>();
  for (const permission of permissions.value) {
    const module = permission.module || "other";
    result.set(module, [...(result.get(module) || []), permission]);
  }
  return [...result.entries()].sort(([left], [right]) =>
    left.localeCompare(right),
  );
});
const auditPages = computed(() =>
  Math.max(1, Math.ceil(auditTotal.value / auditPageSize.value)),
);

function responseMessage(error: unknown, fallback: string) {
  const candidate = error as {
    response?: { data?: { message?: string } };
    message?: string;
  };
  return candidate.response?.data?.message || candidate.message || fallback;
}

function dateTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : date.toLocaleString("zh-CN", { hour12: false });
}

function shortID(value?: string | null) {
  if (!value) return "—";
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-5)}` : value;
}

function normalizeAdmin(item: AdminSummary): AdminSummary {
  const roleList = Array.isArray(item.roles) ? item.roles : [];
  return {
    ...item,
    roles: roleList,
    role_ids:
      Array.isArray(item.role_ids) && item.role_ids.length
        ? item.role_ids
        : roleList.map((role) => role.id),
    totp_enabled: Boolean(item.totp_enabled),
  };
}

function normalizeRole(item: RoleSummary): RoleSummary {
  return {
    ...item,
    permission_ids: Array.isArray(item.permission_ids)
      ? item.permission_ids
      : [],
    admin_count: Number(item.admin_count || 0),
  };
}

async function loadOverview() {
  overviewLoading.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.get("/access/overview");
    admins.value = (data.data?.admins || []).map(normalizeAdmin);
    roles.value = (data.data?.roles || []).map(normalizeRole);
    permissions.value = data.data?.permissions || [];
    currentAdminID.value = data.data?.current_admin_id || currentAdminID.value;
  } catch (error) {
    loadError.value = responseMessage(error, t("access.loadError"));
  } finally {
    overviewLoading.value = false;
  }
}

async function loadAudit() {
  auditLoading.value = true;
  loadError.value = "";
  try {
    const { data } = await adminApi.get("/access/audit-logs", {
      params: {
        page: auditPage.value,
        page_size: auditPageSize.value,
        q: auditSearch.value.trim() || undefined,
        action: auditAction.value.trim() || undefined,
        resource: auditResource.value.trim() || undefined,
        admin_id: auditAdminID.value || undefined,
        from: auditDateFrom.value || undefined,
        to: auditDateTo.value || undefined,
      },
    });
    const page = data.data as PagePayload<AuditLog>;
    auditLogs.value = page?.items || [];
    auditTotal.value = Number(page?.total || 0);
  } catch (error) {
    loadError.value = responseMessage(error, t("access.auditLoadError"));
  } finally {
    auditLoading.value = false;
  }
}

function refreshCurrentTab() {
  if (activeTab.value === "audit") loadAudit();
  else loadOverview();
}

function openCreateAdmin() {
  if (!canManage.value) return;
  editingAdmin.value = null;
  adminForm.value = {
    username: "",
    name: "",
    password: "",
    status: "active",
    role_ids: [],
    reason: "",
  };
  adminError.value = "";
  adminModal.value = true;
}

function openEditAdmin(admin: AdminSummary) {
  if (!canManage.value) return;
  editingAdmin.value = admin;
  adminForm.value = {
    username: admin.username,
    name: admin.name,
    password: "",
    status: admin.status === "disabled" ? "disabled" : "active",
    role_ids: [...admin.role_ids],
    reason: "",
  };
  adminError.value = "";
  adminModal.value = true;
}

function validateReason(reason: string) {
  const count = [...reason.trim()].length;
  return count >= 4 && count <= 500;
}

function strongPassword(password: string) {
  return (
    password.length >= 12 &&
    password.length <= 72 &&
    /[a-z]/.test(password) &&
    /[A-Z]/.test(password) &&
    /\d/.test(password) &&
    /[^A-Za-z0-9]/.test(password)
  );
}

async function saveAdmin() {
  if (!canManage.value) return;
  const form = adminForm.value;
  adminError.value = "";
  if (
    !editingAdmin.value &&
    !/^[a-z][a-z0-9._-]{1,78}[a-z0-9]$/.test(form.username)
  ) {
    adminError.value = t("access.errUsername");
    return;
  }
  if (form.name.trim().length < 2 || form.name.trim().length > 80) {
    adminError.value = t("access.errName");
    return;
  }
  if (!form.role_ids.length) {
    adminError.value = t("access.errNeedRole");
    return;
  }
  if (!editingAdmin.value && !strongPassword(form.password)) {
    adminError.value = t("access.errPassword");
    return;
  }
  if (!validateReason(form.reason)) {
    adminError.value = t("access.errReason");
    return;
  }
  adminSaving.value = true;
  try {
    const headers = { "X-Change-Reason": form.reason.trim() };
    if (editingAdmin.value) {
      const { data } = await adminApi.patch(
        `/access/admins/${editingAdmin.value.id}`,
        {
          name: form.name.trim(),
          status: form.status,
          role_ids: form.role_ids,
        },
        { headers },
      );
      if (
        editingAdmin.value.id === currentAdminID.value &&
        data.data?.security_context_revoked
      ) {
        localStorage.removeItem("linlinqi-admin-token");
        localStorage.removeItem("linlinqi-admin-profile");
        window.location.replace("/login");
        return;
      }
      notice.value = t("access.adminUpdated", {
        username: editingAdmin.value.username,
      });
    } else {
      await adminApi.post(
        "/access/admins",
        {
          username: form.username,
          name: form.name.trim(),
          password: form.password,
          status: form.status,
          role_ids: form.role_ids,
        },
        { headers },
      );
      notice.value = t("access.adminCreated", { username: form.username });
    }
    adminForm.value.password = "";
    adminModal.value = false;
    await loadOverview();
  } catch (error) {
    adminError.value = responseMessage(error, t("access.adminSaveError"));
  } finally {
    adminSaving.value = false;
  }
}

function openPassword(admin: AdminSummary) {
  if (!canManage.value) return;
  passwordAdmin.value = admin;
  currentPassword.value = "";
  newPassword.value = "";
  passwordReason.value = "";
  passwordError.value = "";
  passwordModal.value = true;
}

async function savePassword() {
  if (!canManage.value) return;
  if (!passwordAdmin.value) return;
  passwordError.value = "";
  if (!currentPassword.value) {
    passwordError.value = t("access.errCurrentPassword");
    return;
  }
  if (!strongPassword(newPassword.value)) {
    passwordError.value = t("access.errNewPassword");
    return;
  }
  if (!validateReason(passwordReason.value)) {
    passwordError.value = t("access.errPasswordReason");
    return;
  }
  passwordSaving.value = true;
  try {
    await adminApi.post(
      `/access/admins/${passwordAdmin.value.id}/password`,
      {
        current_password: currentPassword.value,
        new_password: newPassword.value,
      },
      { headers: { "X-Change-Reason": passwordReason.value.trim() } },
    );
    const changedSelf = passwordAdmin.value.id === currentAdminID.value;
    currentPassword.value = "";
    newPassword.value = "";
    passwordModal.value = false;
    if (changedSelf) {
      localStorage.removeItem("linlinqi-admin-token");
      localStorage.removeItem("linlinqi-admin-profile");
      window.location.replace("/login");
      return;
    }
    notice.value = t("access.passwordReset", {
      username: passwordAdmin.value.username,
    });
    await loadOverview();
  } catch (error) {
    passwordError.value = responseMessage(
      error,
      t("access.passwordResetError"),
    );
  } finally {
    passwordSaving.value = false;
  }
}

function openCreateRole() {
  if (!canManage.value) return;
  editingRole.value = null;
  roleForm.value = {
    code: "",
    name: "",
    description: "",
    permission_ids: [],
    reason: "",
  };
  roleError.value = "";
  roleModal.value = true;
}

function openEditRole(role: RoleSummary) {
  if (!canManage.value) return;
  if (role.system) return;
  editingRole.value = role;
  roleForm.value = {
    code: role.code,
    name: role.name,
    description: role.description || "",
    permission_ids: [...role.permission_ids],
    reason: "",
  };
  roleError.value = "";
  roleModal.value = true;
}

async function saveRole() {
  if (!canManage.value) return;
  const form = roleForm.value;
  roleError.value = "";
  if (!/^[a-z][a-z0-9_-]{1,78}[a-z0-9]$/.test(form.code)) {
    roleError.value = t("access.errRoleCode");
    return;
  }
  if (form.name.trim().length < 2 || form.name.trim().length > 120) {
    roleError.value = t("access.errRoleName");
    return;
  }
  if (form.description.trim().length > 500) {
    roleError.value = t("access.errRoleDesc");
    return;
  }
  if (!form.permission_ids.length) {
    roleError.value = t("access.errNeedPermission");
    return;
  }
  if (!validateReason(form.reason)) {
    roleError.value = t("access.errReason");
    return;
  }
  roleSaving.value = true;
  try {
    const body = {
      code: form.code,
      name: form.name.trim(),
      description: form.description.trim(),
      permission_ids: form.permission_ids,
    };
    const headers = { "X-Change-Reason": form.reason.trim() };
    const currentUsesEditedRole = Boolean(
      editingRole.value &&
      admins.value
        .find((admin) => admin.id === currentAdminID.value)
        ?.role_ids.includes(editingRole.value.id),
    );
    if (editingRole.value)
      await adminApi.put(`/access/roles/${editingRole.value.id}`, body, {
        headers,
      });
    else await adminApi.post("/access/roles", body, { headers });
    if (currentUsesEditedRole) {
      localStorage.removeItem("linlinqi-admin-token");
      localStorage.removeItem("linlinqi-admin-profile");
      window.location.replace("/login");
      return;
    }
    notice.value = editingRole.value
      ? t("access.roleUpdated", { name: form.name.trim() })
      : t("access.roleCreated", { name: form.name.trim() });
    roleModal.value = false;
    await loadOverview();
  } catch (error) {
    roleError.value = responseMessage(error, t("access.roleSaveError"));
  } finally {
    roleSaving.value = false;
  }
}

function openDeleteRole(role: RoleSummary) {
  if (!canManage.value) return;
  deletingRole.value = role;
  deleteReason.value = "";
  deleteError.value = "";
  deleteModal.value = true;
}

async function confirmDeleteRole() {
  if (!canManage.value) return;
  if (!deletingRole.value) return;
  if (!validateReason(deleteReason.value)) {
    deleteError.value = t("access.errDeleteReason");
    return;
  }
  deleteSaving.value = true;
  try {
    await adminApi.delete(`/access/roles/${deletingRole.value.id}`, {
      headers: { "X-Change-Reason": deleteReason.value.trim() },
    });
    notice.value = t("access.roleDeleted", { name: deletingRole.value.name });
    deleteModal.value = false;
    await loadOverview();
  } catch (error) {
    deleteError.value = responseMessage(error, t("access.roleDeleteError"));
  } finally {
    deleteSaving.value = false;
  }
}

function rolePermissionNames(role: RoleSummary) {
  const selected = new Set(role.permission_ids);
  return permissions.value
    .filter((permission) => selected.has(permission.id))
    .map((permission) => permission.name);
}

function resetAuditFilters() {
  auditSearch.value = "";
  auditAction.value = "";
  auditResource.value = "";
  auditAdminID.value = "";
  auditDateFrom.value = "";
  auditDateTo.value = "";
  auditPage.value = 1;
  loadAudit();
}

function changeAuditPage(next: number) {
  if (next < 1 || next > auditPages.value || next === auditPage.value) return;
  auditPage.value = next;
  loadAudit();
}

watch(activeTab, (tab) => {
  loadError.value = "";
  if (tab === "audit" && !auditLogs.value.length) loadAudit();
});

watch(
  () => [route.path, route.meta.defaultTab] as const,
  ([, defaultTab]) => {
    const requested = String(defaultTab || "admins") as Tab;
    activeTab.value = ["admins", "roles", "audit"].includes(requested)
      ? requested
      : "admins";
  },
  { immediate: true },
);

onMounted(() => {
  try {
    const profile = JSON.parse(
      localStorage.getItem("linlinqi-admin-profile") || "{}",
    ) as { id?: string };
    currentAdminID.value = profile.id || "";
  } catch {
    currentAdminID.value = "";
  }
  loadOverview();
});
</script>

<template>
  <section class="access-view">
    <header class="access-topbar">
      <div class="access-actions">
        <button
          v-if="activeTab === 'admins' && canManage"
          type="button"
          class="primary-button compact"
          @click="openCreateAdmin"
        >
          <Plus :size="14" />{{ t("access.createAdmin") }}
        </button>
        <button
          v-if="activeTab === 'roles' && canManage"
          type="button"
          class="primary-button compact"
          @click="openCreateRole"
        >
          <Plus :size="14" />{{ t("access.createRole") }}
        </button>
        <button
          type="button"
          class="secondary-button"
          :disabled="overviewLoading || auditLoading"
          @click="refreshCurrentTab"
        >
          <RefreshCw
            :size="14"
            :class="{ spinning: overviewLoading || auditLoading }"
          />{{ t("access.refresh") }}
        </button>
      </div>
    </header>

    <div class="access-metrics">
      <article>
        <span><UserCog :size="15" />{{ t("access.metricActiveAdmins") }}</span
        ><strong>{{ activeAdmins }}</strong
        ><small>{{ t("access.metricActiveAdminsSub") }}</small>
      </article>
      <article>
        <span
          ><LockKeyhole :size="15" />{{ t("access.metricSystemAdmins") }}</span
        ><strong>{{ protectedAdmins }}</strong
        ><small>{{ t("access.metricSystemAdminsSub") }}</small>
      </article>
      <article>
        <span><FileKey :size="15" />{{ t("access.metricCustomRoles") }}</span
        ><strong>{{ customRoles }}</strong
        ><small>{{ t("access.metricCustomRolesSub") }}</small>
      </article>
      <article>
        <span
          ><ShieldCheck :size="15" />{{ t("access.metricPermissions") }}</span
        ><strong>{{ permissions.length }}</strong
        ><small>{{ t("access.metricPermissionsSub") }}</small>
      </article>
    </div>

    <div v-if="notice" class="access-alert success">
      <Check :size="14" /><span>{{ notice }}</span
      ><button type="button" @click="notice = ''"><X :size="13" /></button>
    </div>
    <div v-if="loadError" class="access-alert danger">
      <AlertCircle :size="14" /><span>{{ loadError }}</span
      ><button type="button" @click="refreshCurrentTab">
        {{ t("access.retry") }}
      </button>
    </div>

    <section v-if="activeTab === 'admins'" class="access-panel">
      <header>
        <div>
          <h2>{{ t("access.adminsTitle") }}</h2>
          <p>{{ t("access.adminsSubtitle") }}</p>
        </div>
        <span>{{ t("access.adminCount", { count: admins.length }) }}</span>
      </header>
      <div class="access-table-wrap">
        <table v-if="admins.length">
          <thead>
            <tr>
              <th>{{ t("access.colAdmin") }}</th>
              <th>{{ t("access.colRole") }}</th>
              <th>{{ t("access.colTotp") }}</th>
              <th>{{ t("access.colLastLogin") }}</th>
              <th>{{ t("access.colStatus") }}</th>
              <th>{{ t("access.colActions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="admin in admins" :key="admin.id">
              <td :data-label="t('access.colAdmin')">
                <strong>{{ admin.name || admin.username }}</strong
                ><code>{{ admin.username }}</code
                ><small v-if="admin.id === currentAdminID">{{
                  t("access.currentAccount")
                }}</small>
              </td>
              <td :data-label="t('access.colRole')">
                <div class="chip-list">
                  <span
                    v-for="role in admin.roles"
                    :key="role.id"
                    class="access-chip"
                    >{{ role.name }}</span
                  ><em v-if="!admin.roles.length">{{
                    t("access.unassigned")
                  }}</em>
                </div>
              </td>
              <td :data-label="t('access.colTotp')">
                <span
                  class="status-chip"
                  :class="admin.totp_enabled ? 'success' : 'warning'"
                  >{{
                    admin.totp_enabled
                      ? t("access.totpEnabled")
                      : t("access.totpDisabled")
                  }}</span
                >
              </td>
              <td :data-label="t('access.colLastLogin')">
                {{ dateTime(admin.last_login_at) }}
              </td>
              <td :data-label="t('access.colStatus')">
                <span
                  class="status-chip"
                  :class="admin.status === 'active' ? 'success' : 'danger'"
                  >{{
                    admin.status === "active"
                      ? t("access.statusActive")
                      : t("access.statusDisabled")
                  }}</span
                >
              </td>
              <td :data-label="t('access.colActions')">
                <div v-if="canManage" class="row-actions">
                  <button type="button" @click="openEditAdmin(admin)">
                    <Edit3 :size="13" />{{ t("access.editAccount") }}</button
                  ><button type="button" @click="openPassword(admin)">
                    <KeyRound :size="13" />{{ t("access.resetPassword") }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="access-empty">
          <LoaderCircle
            v-if="overviewLoading"
            :size="25"
            class="spinning"
          /><Users v-else :size="27" /><strong>{{
            overviewLoading ? t("access.loadingAdmins") : t("access.noAdmins")
          }}</strong>
        </div>
      </div>
    </section>

    <section v-else-if="activeTab === 'roles'" class="access-panel">
      <header>
        <div>
          <h2>{{ t("access.rolesTitle") }}</h2>
          <p>{{ t("access.rolesSubtitle") }}</p>
        </div>
        <span>{{ t("access.roleCount", { count: roles.length }) }}</span>
      </header>
      <div class="role-grid">
        <article v-for="role in roles" :key="role.id" class="role-card">
          <header>
            <div>
              <strong>{{ role.name }}</strong
              ><code>{{ role.code }}</code>
            </div>
            <span
              class="status-chip"
              :class="role.system ? 'neutral' : 'success'"
              >{{
                role.system ? t("access.systemRole") : t("access.customRole")
              }}</span
            >
          </header>
          <p>{{ role.description || t("access.noRoleDesc") }}</p>
          <div class="role-permissions">
            <span
              v-for="name in rolePermissionNames(role).slice(0, 8)"
              :key="name"
              >{{ name }}</span
            ><em v-if="rolePermissionNames(role).length > 8">{{
              t("access.moreItems", {
                count: rolePermissionNames(role).length - 8,
              })
            }}</em
            ><em v-if="!role.permission_ids.length">{{
              t("access.noPermissions")
            }}</em>
          </div>
          <footer>
            <span>{{
              t("access.roleStats", {
                admins: role.admin_count,
                perms: role.permission_ids.length,
              })
            }}</span>
            <div v-if="canManage && !role.system">
              <button type="button" @click="openEditRole(role)">
                <Edit3 :size="13" />{{ t("access.edit") }}</button
              ><button
                type="button"
                class="danger"
                :disabled="role.admin_count > 0"
                @click="openDeleteRole(role)"
              >
                <Trash2 :size="13" />{{ t("access.delete") }}
              </button>
            </div>
          </footer>
        </article>
        <div v-if="!roles.length" class="access-empty">
          <LoaderCircle
            v-if="overviewLoading"
            :size="25"
            class="spinning"
          /><FileKey v-else :size="27" /><strong>{{
            overviewLoading ? t("access.loadingRoles") : t("access.noRoles")
          }}</strong>
        </div>
      </div>
    </section>

    <section v-else class="access-panel audit-panel">
      <header>
        <div>
          <h2>{{ t("access.auditTitle") }}</h2>
          <p>{{ t("access.auditSubtitle") }}</p>
        </div>
        <span>{{ t("access.auditCount", { count: auditTotal }) }}</span>
      </header>
      <div class="audit-filters">
        <div>
          <Search :size="14" /><input
            v-model="auditSearch"
            :placeholder="t('access.auditSearchPlaceholder')"
            @keydown.enter="
              auditPage = 1;
              loadAudit();
            "
          />
        </div>
        <input
          v-model="auditAction"
          :placeholder="t('access.auditActionPlaceholder')"
          @keydown.enter="
            auditPage = 1;
            loadAudit();
          "
        />
        <input
          v-model="auditResource"
          :placeholder="t('access.auditResourcePlaceholder')"
          @keydown.enter="
            auditPage = 1;
            loadAudit();
          "
        />
        <select v-model="auditAdminID">
          <option value="">{{ t("access.allAdmins") }}</option>
          <option v-for="admin in admins" :key="admin.id" :value="admin.id">
            {{ admin.name || admin.username }}
          </option>
        </select>
        <input v-model="auditDateFrom" type="date" />
        <input v-model="auditDateTo" type="date" />
        <button
          type="button"
          class="primary-button compact"
          @click="
            auditPage = 1;
            loadAudit();
          "
        >
          {{ t("access.apply") }}</button
        ><button type="button" class="text-button" @click="resetAuditFilters">
          {{ t("access.reset") }}
        </button>
      </div>
      <div class="access-table-wrap">
        <table v-if="auditLogs.length">
          <thead>
            <tr>
              <th>{{ t("access.colAction") }}</th>
              <th>{{ t("access.colAdmin") }}</th>
              <th>{{ t("access.colIp") }}</th>
              <th>{{ t("access.colDetail") }}</th>
              <th>{{ t("access.colTime") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in auditLogs" :key="log.id">
              <td :data-label="t('access.colAction')">
                <strong>{{ log.action }}</strong
                ><code
                  >{{ log.resource }} · {{ shortID(log.resource_id) }}</code
                >
              </td>
              <td :data-label="t('access.colAdmin')">
                <strong>{{
                  log.admin_name || log.admin_username || t("access.system")
                }}</strong
                ><code v-if="log.admin_id">{{ shortID(log.admin_id) }}</code>
              </td>
              <td :data-label="t('access.colIp')">
                <code>{{ log.ip || "—" }}</code>
              </td>
              <td :data-label="t('access.colDetail')">
                <p class="audit-detail">{{ log.detail || "—" }}</p>
              </td>
              <td :data-label="t('access.colTime')">
                {{ dateTime(log.created_at) }}
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="access-empty">
          <LoaderCircle
            v-if="auditLoading"
            :size="25"
            class="spinning"
          /><ShieldCheck v-else :size="27" /><strong>{{
            auditLoading ? t("access.loadingAudit") : t("access.noAuditLogs")
          }}</strong>
        </div>
      </div>
      <footer class="access-pagination">
        <span>{{
          t("access.pageInfo", { current: auditPage, total: auditPages })
        }}</span>
        <div>
          <button
            type="button"
            :disabled="auditPage <= 1 || auditLoading"
            @click="changeAuditPage(auditPage - 1)"
          >
            <ChevronLeft :size="14" />{{ t("access.prevPage") }}</button
          ><button
            type="button"
            :disabled="auditPage >= auditPages || auditLoading"
            @click="changeAuditPage(auditPage + 1)"
          >
            {{ t("access.nextPage") }}<ChevronRight :size="14" />
          </button>
        </div>
      </footer>
    </section>

    <div
      v-if="adminModal && canManage"
      class="access-modal-backdrop"
      @mousedown.self="!adminSaving && (adminModal = false)"
    >
      <section class="access-modal">
        <header>
          <div>
            <span><UserCog :size="18" /></span>
            <div>
              <h2>
                {{
                  editingAdmin ? t("access.editAdmin") : t("access.createAdmin")
                }}
              </h2>
              <p>{{ t("access.adminModalHint") }}</p>
            </div>
          </div>
          <button
            type="button"
            :disabled="adminSaving"
            @click="
              adminModal = false;
              adminForm.password = '';
            "
          >
            <X :size="16" />
          </button>
        </header>
        <form class="access-form" @submit.prevent="saveAdmin">
          <div class="form-grid">
            <label
              ><span>{{ t("access.username") }}</span
              ><input
                v-model="adminForm.username"
                :disabled="Boolean(editingAdmin)"
                maxlength="80"
                autocomplete="off" /></label
            ><label
              ><span>{{ t("access.name") }}</span
              ><input v-model="adminForm.name" maxlength="80" /></label
            ><label v-if="!editingAdmin"
              ><span>{{ t("access.initialPassword") }}</span
              ><input
                v-model="adminForm.password"
                type="password"
                maxlength="72"
                autocomplete="new-password"
              /><small>{{ t("access.passwordHint") }}</small></label
            ><label
              ><span>{{ t("access.accountStatus") }}</span
              ><select
                v-model="adminForm.status"
                :disabled="editingAdmin?.id === currentAdminID"
              >
                <option value="active">{{ t("access.statusActive") }}</option>
                <option value="disabled">
                  {{ t("access.statusDisableOption") }}
                </option>
              </select></label
            >
          </div>
          <fieldset>
            <legend>{{ t("access.assignRolesLegend") }}</legend>
            <div class="choice-grid">
              <label v-for="role in roles" :key="role.id" class="choice-card"
                ><input
                  v-model="adminForm.role_ids"
                  type="checkbox"
                  :value="role.id"
                /><span
                  ><strong>{{ role.name }}</strong
                  ><small
                    >{{ role.code }} ·
                    {{
                      t("access.permissionCount", {
                        count: role.permission_ids.length,
                      })
                    }}</small
                  ></span
                ></label
              >
            </div>
          </fieldset>
          <label
            ><span>{{ t("access.changeReason") }}</span
            ><textarea
              v-model="adminForm.reason"
              rows="3"
              maxlength="500"
              :placeholder="t('access.adminReasonPlaceholder')"
            ></textarea>
          </label>
          <div v-if="adminError" class="inline-error">
            <AlertCircle :size="14" />{{ adminError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="adminSaving"
              @click="
                adminModal = false;
                adminForm.password = '';
              "
            >
              {{ t("access.cancel") }}</button
            ><button
              type="submit"
              class="primary-button"
              :disabled="adminSaving"
            >
              <LoaderCircle
                v-if="adminSaving"
                :size="14"
                class="spinning"
              /><Check v-else :size="14" />{{
                adminSaving ? t("access.saving") : t("access.confirmSave")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="roleModal && canManage"
      class="access-modal-backdrop"
      @mousedown.self="!roleSaving && (roleModal = false)"
    >
      <section class="access-modal wide">
        <header>
          <div>
            <span><FileKey :size="18" /></span>
            <div>
              <h2>
                {{
                  editingRole
                    ? t("access.editCustomRole")
                    : t("access.createCustomRole")
                }}
              </h2>
              <p>{{ t("access.roleModalHint") }}</p>
            </div>
          </div>
          <button
            type="button"
            :disabled="roleSaving"
            @click="roleModal = false"
          >
            <X :size="16" />
          </button>
        </header>
        <form class="access-form" @submit.prevent="saveRole">
          <div class="form-grid">
            <label
              ><span>{{ t("access.roleCode") }}</span
              ><input
                v-model="roleForm.code"
                maxlength="80"
                placeholder="finance_auditor" /></label
            ><label
              ><span>{{ t("access.roleName") }}</span
              ><input v-model="roleForm.name" maxlength="120"
            /></label>
          </div>
          <label
            ><span>{{ t("access.roleDesc") }}</span
            ><textarea
              v-model="roleForm.description"
              rows="2"
              maxlength="500"
            ></textarea>
          </label>
          <fieldset>
            <legend>{{ t("access.permissionMatrixLegend") }}</legend>
            <div class="permission-groups">
              <section
                v-for="[module, items] in permissionGroups"
                :key="module"
              >
                <header>
                  <strong>{{ module }}</strong
                  ><button
                    type="button"
                    @click="
                      roleForm.permission_ids = [
                        ...new Set([
                          ...roleForm.permission_ids,
                          ...items.map((item) => item.id),
                        ]),
                      ]
                    "
                  >
                    {{ t("access.selectAllModule") }}
                  </button>
                </header>
                <label v-for="permission in items" :key="permission.id"
                  ><input
                    v-model="roleForm.permission_ids"
                    type="checkbox"
                    :value="permission.id"
                  /><span
                    ><strong>{{ permission.name }}</strong
                    ><code>{{ permission.code }}</code
                    ><small>{{
                      permission.description || t("access.serverControlled")
                    }}</small></span
                  ></label
                >
              </section>
            </div>
          </fieldset>
          <label
            ><span>{{ t("access.changeReason") }}</span
            ><textarea
              v-model="roleForm.reason"
              rows="3"
              maxlength="500"
              :placeholder="t('access.roleReasonPlaceholder')"
            ></textarea>
          </label>
          <div v-if="roleError" class="inline-error">
            <AlertCircle :size="14" />{{ roleError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="roleSaving"
              @click="roleModal = false"
            >
              {{ t("access.cancel") }}</button
            ><button
              type="submit"
              class="primary-button"
              :disabled="roleSaving"
            >
              <LoaderCircle
                v-if="roleSaving"
                :size="14"
                class="spinning"
              /><Check v-else :size="14" />{{
                roleSaving ? t("access.saving") : t("access.confirmSave")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="passwordModal && passwordAdmin && canManage"
      class="access-modal-backdrop"
      @mousedown.self="!passwordSaving && (passwordModal = false)"
    >
      <section class="access-modal small">
        <header>
          <div>
            <span><KeyRound :size="18" /></span>
            <div>
              <h2>{{ t("access.resetPasswordTitle") }}</h2>
              <p>
                {{ passwordAdmin.name || passwordAdmin.username }} ·
                {{ t("access.resetPasswordHint") }}
              </p>
            </div>
          </div>
          <button
            type="button"
            :disabled="passwordSaving"
            @click="
              passwordModal = false;
              currentPassword = '';
              newPassword = '';
            "
          >
            <X :size="16" />
          </button>
        </header>
        <form class="access-form" @submit.prevent="savePassword">
          <label
            ><span>{{ t("access.currentPassword") }}</span
            ><input
              v-model="currentPassword"
              type="password"
              maxlength="72"
              autocomplete="current-password" /></label
          ><label
            ><span>{{ t("access.newPassword") }}</span
            ><input
              v-model="newPassword"
              type="password"
              maxlength="72"
              autocomplete="new-password"
            /><small>{{ t("access.passwordHint") }}</small></label
          ><label
            ><span>{{ t("access.resetReason") }}</span
            ><textarea
              v-model="passwordReason"
              rows="3"
              maxlength="500"
            ></textarea>
          </label>
          <div v-if="passwordError" class="inline-error">
            <AlertCircle :size="14" />{{ passwordError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="passwordSaving"
              @click="
                passwordModal = false;
                currentPassword = '';
                newPassword = '';
              "
            >
              {{ t("access.cancel") }}</button
            ><button
              type="submit"
              class="primary-button"
              :disabled="passwordSaving"
            >
              <LoaderCircle
                v-if="passwordSaving"
                :size="14"
                class="spinning"
              /><KeyRound v-else :size="14" />{{
                passwordSaving
                  ? t("access.resetting")
                  : t("access.confirmReset")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="deleteModal && deletingRole && canManage"
      class="access-modal-backdrop"
      @mousedown.self="!deleteSaving && (deleteModal = false)"
    >
      <section class="access-modal small">
        <header>
          <div>
            <span class="danger-icon"><Trash2 :size="18" /></span>
            <div>
              <h2>{{ t("access.deleteRoleTitle") }}</h2>
              <p>{{ deletingRole.name }} · {{ deletingRole.code }}</p>
            </div>
          </div>
          <button
            type="button"
            :disabled="deleteSaving"
            @click="deleteModal = false"
          >
            <X :size="16" />
          </button>
        </header>
        <form class="access-form" @submit.prevent="confirmDeleteRole">
          <p class="delete-warning">
            {{ t("access.deleteRoleWarning") }}
          </p>
          <label
            ><span>{{ t("access.deleteReason") }}</span
            ><textarea
              v-model="deleteReason"
              rows="3"
              maxlength="500"
            ></textarea>
          </label>
          <div v-if="deleteError" class="inline-error">
            <AlertCircle :size="14" />{{ deleteError }}
          </div>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="deleteSaving"
              @click="deleteModal = false"
            >
              {{ t("access.cancel") }}</button
            ><button
              type="submit"
              class="danger-button"
              :disabled="deleteSaving"
            >
              <LoaderCircle
                v-if="deleteSaving"
                :size="14"
                class="spinning"
              /><Trash2 v-else :size="14" />{{
                deleteSaving ? t("access.deleting") : t("access.confirmDelete")
              }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.access-view {
  display: grid;
  gap: 14px;
  padding: 18px;
}
.access-topbar {
  min-height: 58px;
  padding: 10px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.access-tabs,
.access-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}
.access-tabs button,
.secondary-button,
.text-button,
.row-actions button,
.role-card footer button,
.access-pagination button {
  min-height: 34px;
  padding: 0 11px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 10px;
  font-weight: 650;
}
.access-tabs button.active {
  border-color: var(--dark);
  background: var(--dark);
  color: var(--dark-text);
}
.access-actions .primary-button {
  min-width: 120px;
}
.access-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.access-metrics article {
  min-height: 112px;
  padding: 14px;
  display: grid;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.access-metrics span {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 9px;
}
.access-metrics strong {
  font-size: 24px;
}
.access-metrics small {
  color: var(--muted);
  font-size: 9px;
}
.access-alert {
  min-height: 40px;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 8px;
  font-size: 10px;
}
.access-alert span {
  flex: 1;
}
.access-alert button {
  border: 0;
  background: transparent;
  color: inherit;
}
.access-alert.success {
  border-color: color-mix(in srgb, var(--success) 30%, var(--line));
  background: color-mix(in srgb, var(--success) 7%, var(--surface));
  color: var(--success);
}
.access-alert.danger {
  border-color: color-mix(in srgb, var(--danger) 30%, var(--line));
  background: color-mix(in srgb, var(--danger) 7%, var(--surface));
  color: var(--danger);
}
.access-panel {
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.access-panel > header {
  min-height: 65px;
  padding: 13px 15px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
}
.access-panel h2 {
  margin: 0 0 4px;
  font-size: 13px;
}
.access-panel header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
}
.access-panel > header > span {
  color: var(--muted);
  font-size: 9px;
}
.access-table-wrap {
  overflow: auto;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th {
  padding: 9px 12px;
  text-align: left;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 8px;
  letter-spacing: 0.04em;
  white-space: nowrap;
}
td {
  padding: 11px 12px;
  border-top: 1px solid var(--line);
  font-size: 10px;
  vertical-align: middle;
}
td:first-child {
  min-width: 150px;
}
td strong,
td code,
td small {
  display: block;
}
td code {
  margin-top: 3px;
  color: var(--muted);
  font-size: 8px;
}
td small {
  margin-top: 3px;
  color: var(--success);
  font-size: 8px;
}
.chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  min-width: 150px;
}
.access-chip,
.role-permissions span {
  padding: 4px 7px;
  border-radius: 10px;
  background: var(--soft);
  color: var(--text);
  font-size: 8px;
  font-style: normal;
}
.chip-list em,
.role-permissions em {
  color: var(--muted);
  font-size: 8px;
  font-style: normal;
}
.status-chip {
  padding: 4px 7px;
  display: inline-flex;
  border-radius: 10px;
  font-size: 8px;
  font-weight: 700;
  white-space: nowrap;
}
.status-chip.success {
  background: color-mix(in srgb, var(--success) 10%, var(--surface));
  color: var(--success);
}
.status-chip.warning {
  background: color-mix(in srgb, var(--warn) 10%, var(--surface));
  color: var(--warn);
}
.status-chip.danger {
  background: color-mix(in srgb, var(--danger) 10%, var(--surface));
  color: var(--danger);
}
.status-chip.neutral {
  background: var(--soft);
  color: var(--muted);
}
.row-actions {
  display: flex;
  gap: 5px;
}
.row-actions button {
  min-height: 29px;
  padding: 0 7px;
  white-space: nowrap;
}
.role-grid {
  padding: 14px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 11px;
}
.role-card {
  min-width: 0;
  min-height: 225px;
  padding: 14px;
  display: grid;
  grid-template-rows: auto auto 1fr auto;
  gap: 11px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface-2);
}
.role-card > header {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}
.role-card header div {
  min-width: 0;
}
.role-card header strong {
  display: block;
  font-size: 12px;
}
.role-card header code {
  display: block;
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}
.role-card > p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.6;
}
.role-permissions {
  align-content: start;
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.role-card footer {
  padding-top: 9px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border-top: 1px solid var(--line);
}
.role-card footer > span {
  color: var(--muted);
  font-size: 8px;
}
.role-card footer div {
  display: flex;
  gap: 5px;
}
.role-card footer button {
  min-height: 28px;
  padding: 0 7px;
}
.role-card footer button.danger {
  color: var(--danger);
}
.audit-filters {
  padding: 11px 13px;
  display: grid;
  grid-template-columns: 1.2fr 1fr 1fr 1fr 0.75fr 0.75fr auto auto;
  gap: 7px;
  border-bottom: 1px solid var(--line);
}
.audit-filters > div {
  position: relative;
}
.audit-filters > div svg {
  position: absolute;
  left: 9px;
  top: 10px;
  color: var(--muted);
}
.audit-filters input,
.audit-filters select {
  width: 100%;
  height: 34px;
  padding: 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  outline: 0;
  background: var(--surface-2);
  font-size: 9px;
}
.audit-filters > div input {
  padding-left: 29px;
}
.audit-detail {
  max-width: 430px;
  margin: 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.55;
  white-space: normal;
  overflow-wrap: anywhere;
}
.access-pagination {
  padding: 10px 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--line);
  color: var(--muted);
  font-size: 9px;
}
.access-pagination div {
  display: flex;
  gap: 5px;
}
.access-pagination button:disabled,
button:disabled {
  cursor: not-allowed;
  opacity: 0.48;
}
.access-empty {
  min-height: 250px;
  grid-column: 1 / -1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
}
.access-empty strong {
  font-size: 10px;
}
.access-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
  padding: 22px;
  display: grid;
  place-items: center;
  background: rgba(0, 0, 0, 0.48);
  backdrop-filter: blur(3px);
}
.access-modal {
  width: min(680px, 100%);
  max-height: calc(100vh - 44px);
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 11px;
  background: var(--surface);
  box-shadow: 0 25px 80px rgba(0, 0, 0, 0.25);
}
.access-modal.wide {
  width: min(920px, 100%);
}
.access-modal.small {
  width: min(520px, 100%);
}
.access-modal > header {
  position: sticky;
  top: 0;
  z-index: 2;
  min-height: 68px;
  padding: 13px 15px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}
.access-modal > header > div {
  display: flex;
  align-items: center;
  gap: 10px;
}
.access-modal > header > div > span {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: var(--soft);
}
.access-modal > header > div > span.danger-icon {
  color: var(--danger);
}
.access-modal h2 {
  margin: 0 0 4px;
  font-size: 13px;
}
.access-modal header p {
  margin: 0;
  color: var(--muted);
  font-size: 9px;
}
.access-modal > header > button {
  width: 31px;
  height: 31px;
  display: grid;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--surface-2);
}
.access-form {
  padding: 15px;
  display: grid;
  gap: 13px;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.access-form > label,
.form-grid label {
  display: grid;
  gap: 6px;
}
.access-form label > span,
.form-grid label > span {
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
}
.access-form input:not([type="checkbox"]),
.access-form select,
.access-form textarea {
  width: 100%;
  padding: 9px 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  outline: 0;
  background: var(--surface-2);
  font-size: 10px;
}
.access-form input:not([type="checkbox"]),
.access-form select {
  height: 38px;
}
.access-form textarea {
  resize: vertical;
  line-height: 1.55;
}
.access-form input:focus,
.access-form select:focus,
.access-form textarea:focus {
  border-color: var(--dark);
}
.access-form label small {
  color: var(--muted);
  font-size: 8px;
}
.access-form fieldset {
  margin: 0;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 8px;
}
.access-form legend {
  padding: 0 5px;
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
}
.choice-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 7px;
}
.choice-card {
  min-height: 52px;
  padding: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
  cursor: pointer;
}
.choice-card span {
  display: grid;
  gap: 3px;
}
.choice-card strong {
  font-size: 10px;
}
.choice-card small {
  color: var(--muted);
  font-size: 8px;
}
.permission-groups {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 9px;
}
.permission-groups section {
  border: 1px solid var(--line);
  border-radius: 7px;
  overflow: hidden;
}
.permission-groups section > header {
  min-height: 35px;
  padding: 6px 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  background: var(--soft);
}
.permission-groups section > header strong {
  text-transform: uppercase;
  font-size: 8px;
  letter-spacing: 0.08em;
}
.permission-groups section > header button {
  border: 0;
  background: transparent;
  color: var(--muted);
  font-size: 8px;
}
.permission-groups section > label {
  min-height: 49px;
  padding: 7px 8px;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  border-top: 1px solid var(--line);
  cursor: pointer;
}
.permission-groups label > span {
  display: grid;
  gap: 2px;
}
.permission-groups label strong {
  font-size: 9px;
}
.permission-groups label code,
.permission-groups label small {
  color: var(--muted);
  font-size: 7.5px;
}
.inline-error {
  padding: 9px 10px;
  display: flex;
  align-items: center;
  gap: 7px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
  color: var(--danger);
  font-size: 9px;
}
.access-form > footer {
  padding-top: 11px;
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  border-top: 1px solid var(--line);
}
.access-form > footer .primary-button,
.access-form > footer .secondary-button,
.danger-button {
  min-width: 110px;
  height: 36px;
}
.danger-button {
  border: 0;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--danger);
  color: white;
  font-size: 10px;
  font-weight: 700;
}
.delete-warning {
  margin: 0;
  padding: 10px;
  border-radius: 7px;
  background: color-mix(in srgb, var(--danger) 7%, var(--surface));
  color: var(--danger);
  font-size: 9px;
  line-height: 1.6;
}
.spinning {
  animation: access-spin 0.8s linear infinite;
}
@keyframes access-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 1100px) {
  .access-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .role-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .audit-filters {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}
@media (max-width: 720px) {
  .access-view {
    padding: 12px;
  }
  .access-topbar {
    align-items: stretch;
    flex-direction: column;
  }
  .access-tabs {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
  }
  .access-actions {
    justify-content: flex-end;
  }
  .role-grid {
    grid-template-columns: 1fr;
  }
  .audit-filters {
    grid-template-columns: 1fr 1fr;
  }
  table,
  thead,
  tbody,
  tr,
  th,
  td {
    display: block;
  }
  thead {
    display: none;
  }
  tbody {
    display: grid;
    gap: 8px;
    padding: 9px;
  }
  tr {
    padding: 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--surface-2);
  }
  td {
    padding: 7px 0;
    display: grid;
    grid-template-columns: 105px 1fr;
    border: 0;
  }
  td:before {
    content: attr(data-label);
    color: var(--muted);
    font-size: 8px;
  }
  .row-actions {
    flex-wrap: wrap;
  }
  .audit-detail {
    max-width: none;
  }
  .permission-groups {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 500px) {
  .access-metrics {
    grid-template-columns: 1fr;
  }
  .access-tabs {
    grid-template-columns: 1fr;
  }
  .access-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
  .form-grid,
  .choice-grid,
  .audit-filters {
    grid-template-columns: 1fr;
  }
  .access-modal-backdrop {
    padding: 8px;
  }
  .access-modal {
    max-height: calc(100vh - 16px);
  }
  .access-form > footer {
    display: grid;
    grid-template-columns: 1fr 1fr;
  }
  .role-card footer {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
