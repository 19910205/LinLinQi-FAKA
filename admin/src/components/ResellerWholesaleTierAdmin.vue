<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  AlertCircle,
  BadgePercent,
  Check,
  Edit3,
  LoaderCircle,
  Plus,
  RefreshCw,
  ShieldCheck,
  Users,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";

interface ResellerWholesaleTier {
  id: string;
  level: number;
  name: string;
  discount_basis_point: number;
  enabled: boolean;
  active_reseller_count: number;
  total_reseller_count: number;
  created_at: string;
  updated_at: string;
}

const emit = defineEmits<{
  updated: [];
}>();

const { t, locale } = useI18n();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("reseller.manage"));
const tiers = ref<ResellerWholesaleTier[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const modalOpen = ref(false);
const editingLevel = ref<number | null>(null);
const form = reactive({
  level: 0,
  name: "",
  discountPercent: 0,
  enabled: true,
  reason: "",
});

const configuredLevels = computed(
  () => new Set(tiers.value.map((item) => item.level)),
);
const availableLevels = computed(() =>
  Array.from({ length: 11 }, (_, level) => level).filter(
    (level) => !configuredLevels.value.has(level),
  ),
);
const hasAvailableLevel = computed(() => availableLevels.value.length > 0);

watch(
  () => form.level,
  (level) => {
    if (level !== 0) return;
    form.discountPercent = 0;
    form.enabled = true;
  },
);

function apiMessage(value: unknown, fallback: string) {
  const failure = value as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function formatPercent(basisPoints: number) {
  return `${(Number(basisPoints || 0) / 100).toLocaleString(locale.value, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })}%`;
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString(locale.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const response = await adminApi.get("/operations/reseller-wholesale-tiers");
    const payload = response.data?.data;
    tiers.value = (Array.isArray(payload) ? payload : []).sort(
      (left, right) => left.level - right.level,
    );
  } catch (caught: unknown) {
    tiers.value = [];
    error.value = apiMessage(caught, t("reselleradmin.tiers.errLoad"));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  if (!canManage.value) return;
  const level = availableLevels.value[0];
  if (level === undefined) return;
  editingLevel.value = null;
  Object.assign(form, {
    level,
    name: "",
    discountPercent: 0,
    enabled: true,
    reason: "",
  });
  error.value = "";
  modalOpen.value = true;
}

function openEdit(item: ResellerWholesaleTier) {
  if (!canManage.value) return;
  editingLevel.value = item.level;
  Object.assign(form, {
    level: item.level,
    name: item.name,
    discountPercent: Number((item.discount_basis_point / 100).toFixed(2)),
    enabled: item.enabled,
    reason: "",
  });
  error.value = "";
  modalOpen.value = true;
}

function closeModal() {
  if (saving.value) return;
  modalOpen.value = false;
  editingLevel.value = null;
  error.value = "";
}

function validate() {
  if (!Number.isInteger(form.level) || form.level < 0 || form.level > 10)
    return t("reselleradmin.tiers.errLevel");
  const nameLength = [...form.name.trim()].length;
  if (nameLength < 2 || nameLength > 100)
    return t("reselleradmin.tiers.errName");
  if (
    !Number.isFinite(form.discountPercent) ||
    form.discountPercent < 0 ||
    form.discountPercent > 100 ||
    Math.abs(
      form.discountPercent * 100 - Math.round(form.discountPercent * 100),
    ) > 0.000001
  )
    return t("reselleradmin.tiers.errDiscount");
  if (form.level === 0 && (form.discountPercent !== 0 || !form.enabled))
    return t("reselleradmin.tiers.errBaseline");
  const reasonLength = [...form.reason.trim()].length;
  if (reasonLength < 4 || reasonLength > 500)
    return t("reselleradmin.tiers.errReason");
  return "";
}

async function save() {
  if (!canManage.value) return;
  const validation = validate();
  if (validation) {
    error.value = validation;
    return;
  }
  saving.value = true;
  error.value = "";
  try {
    await adminApi.put(
      `/reseller-wholesale-tiers/${encodeURIComponent(String(form.level))}`,
      {
        name: form.name.trim(),
        discount_basis_point: Math.round(form.discountPercent * 100),
        enabled: form.level === 0 ? true : form.enabled,
      },
      { headers: { "X-Change-Reason": form.reason.trim() } },
    );
    notice.value = t("reselleradmin.tiers.saved", { level: form.level });
    modalOpen.value = false;
    editingLevel.value = null;
    await load();
    emit("updated");
  } catch (caught: unknown) {
    error.value = apiMessage(caught, t("reselleradmin.tiers.errSave"));
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>

<template>
  <section class="tier-admin panel">
    <header class="tier-header">
      <div>
        <span>{{ t("adminKicker.settlementPolicy") }}</span>
        <h2>{{ t("reselleradmin.tiers.title") }}</h2>
        <p>{{ t("reselleradmin.tiers.description") }}</p>
      </div>
      <div class="tier-actions">
        <button type="button" :disabled="loading" @click="load">
          <RefreshCw :class="{ spinning: loading }" />
          {{ t("reselleradmin.refresh") }}
        </button>
        <button
          v-if="canManage"
          class="primary-button"
          type="button"
          :disabled="loading || !hasAvailableLevel"
          @click="openCreate"
        >
          <Plus />{{ t("reselleradmin.tiers.configure") }}
        </button>
      </div>
    </header>

    <div class="policy-note">
      <ShieldCheck />
      <div>
        <b>{{ t("reselleradmin.tiers.ruleTitle") }}</b>
        <span>{{ t("reselleradmin.tiers.ruleBody") }}</span>
      </div>
    </div>

    <div v-if="notice" class="tier-feedback success" role="status">
      <Check />{{ notice }}
    </div>
    <div v-if="error && !modalOpen" class="tier-feedback danger" role="alert">
      <AlertCircle />{{ error }}
      <button type="button" @click="load">
        {{ t("reselleradmin.retry") }}
      </button>
    </div>

    <div v-if="loading && !tiers.length" class="tier-state">
      <LoaderCircle class="spinning" />
      <span>{{ t("reselleradmin.tiers.loading") }}</span>
    </div>
    <div v-else-if="!error && !tiers.length" class="tier-state">
      <BadgePercent />
      <b>{{ t("reselleradmin.tiers.empty") }}</b>
      <span>{{ t("reselleradmin.tiers.emptyHint") }}</span>
    </div>
    <div v-else class="tier-grid">
      <article v-for="item in tiers" :key="item.id || item.level">
        <header>
          <div class="level-mark">L{{ item.level }}</div>
          <div>
            <h3>{{ item.name }}</h3>
            <span :class="item.enabled ? 'enabled' : 'disabled'">
              {{
                item.enabled
                  ? t("reselleradmin.tiers.enabled")
                  : t("reselleradmin.tiers.disabled")
              }}
            </span>
          </div>
          <button v-if="canManage" type="button" @click="openEdit(item)">
            <Edit3 />{{ t("reselleradmin.tiers.edit") }}
          </button>
        </header>
        <div class="discount-value">
          <span>{{ t("reselleradmin.tiers.settlementDiscount") }}</span>
          <strong>{{ formatPercent(item.discount_basis_point) }}</strong>
        </div>
        <dl>
          <div>
            <dt><Users />{{ t("reselleradmin.tiers.activeAssignments") }}</dt>
            <dd>{{ item.active_reseller_count || 0 }}</dd>
          </div>
          <div>
            <dt>{{ t("reselleradmin.tiers.totalAssignments") }}</dt>
            <dd>{{ item.total_reseller_count || 0 }}</dd>
          </div>
          <div>
            <dt>{{ t("reselleradmin.tiers.updatedAt") }}</dt>
            <dd>{{ formatTime(item.updated_at) }}</dd>
          </div>
        </dl>
      </article>
    </div>

    <div
      v-if="modalOpen && canManage"
      class="tier-modal-backdrop"
      role="presentation"
      @mousedown.self="closeModal"
    >
      <section
        class="tier-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="t('reselleradmin.tiers.modalAria')"
      >
        <header>
          <div>
            <span>{{ t("adminKicker.wholesaleTier") }}</span>
            <h2>
              {{
                editingLevel === null
                  ? t("reselleradmin.tiers.createTitle")
                  : t("reselleradmin.tiers.editTitle", { level: form.level })
              }}
            </h2>
          </div>
          <button type="button" :disabled="saving" @click="closeModal">
            <X />
          </button>
        </header>

        <form @submit.prevent="save">
          <div v-if="error" class="tier-feedback danger" role="alert">
            <AlertCircle />{{ error }}
          </div>
          <label>
            {{ t("reselleradmin.tiers.levelLabel") }}
            <select
              v-model.number="form.level"
              :disabled="editingLevel !== null || saving"
            >
              <template v-if="editingLevel !== null">
                <option :value="editingLevel">L{{ editingLevel }}</option>
              </template>
              <template v-else>
                <option
                  v-for="level in availableLevels"
                  :key="level"
                  :value="level"
                >
                  L{{ level }}
                </option>
              </template>
            </select>
          </label>
          <label>
            {{ t("reselleradmin.tiers.nameLabel") }}
            <input
              v-model="form.name"
              maxlength="100"
              :disabled="saving"
              :placeholder="t('reselleradmin.tiers.namePlaceholder')"
            />
          </label>
          <label>
            {{ t("reselleradmin.tiers.discountLabel") }}
            <div class="percent-input">
              <input
                v-model.number="form.discountPercent"
                type="number"
                min="0"
                max="100"
                step="0.01"
                :disabled="saving || form.level === 0"
              />
              <span>%</span>
            </div>
            <small>{{ t("reselleradmin.tiers.discountHint") }}</small>
          </label>
          <label class="enabled-control">
            <input
              v-model="form.enabled"
              type="checkbox"
              :disabled="saving || form.level === 0"
            />
            <span>
              <b>{{ t("reselleradmin.tiers.enabledLabel") }}</b>
              <small>{{ t("reselleradmin.tiers.enabledHint") }}</small>
            </span>
          </label>
          <label>
            {{ t("reselleradmin.auditReason") }}
            <textarea
              v-model="form.reason"
              maxlength="500"
              :disabled="saving"
              :placeholder="t('reselleradmin.tiers.reasonPlaceholder')"
            ></textarea>
          </label>
          <p class="baseline-note">
            {{
              form.level === 0
                ? t("reselleradmin.tiers.baselineHint")
                : t("reselleradmin.tiers.effectHint")
            }}
          </p>
          <footer>
            <button type="button" :disabled="saving" @click="closeModal">
              {{ t("reselleradmin.cancel") }}
            </button>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spinning" />
              <Check v-else />{{ t("reselleradmin.tiers.save") }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </section>
</template>

<style scoped>
.tier-admin {
  min-width: 0;
  overflow: hidden;
}

.tier-header {
  padding: 18px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 18px;
}

.tier-header > div:first-child > span,
.tier-modal > header span {
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.14em;
}

.tier-header h2,
.tier-modal h2 {
  margin: 5px 0 0;
  font-size: 17px;
  letter-spacing: -0.03em;
}

.tier-header p {
  max-width: 680px;
  margin: 7px 0 0;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.65;
}

.tier-actions {
  display: flex;
  gap: 7px;
}

.tier-actions button,
.tier-grid article header button,
.tier-modal > header > button,
.tier-modal footer button {
  min-height: 32px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  background: var(--surface);
  color: var(--text);
  font-size: 8px;
}

.tier-actions button svg,
.tier-grid article header button svg,
.tier-modal button svg {
  width: 13px;
}

.tier-actions button:disabled,
.tier-modal button:disabled,
.tier-modal input:disabled,
.tier-modal select:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.policy-note {
  margin: 12px 14px 0;
  padding: 11px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: flex;
  gap: 9px;
  background: var(--surface-2);
}

.policy-note > svg {
  width: 17px;
  flex: 0 0 auto;
}

.policy-note div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.policy-note b {
  font-size: 9px;
}

.policy-note span {
  color: var(--muted);
  font-size: 8px;
  line-height: 1.55;
}

.tier-feedback {
  margin: 11px 14px 0;
  padding: 9px 10px;
  border-radius: 5px;
  display: flex;
  align-items: flex-start;
  gap: 7px;
  font-size: 8px;
  line-height: 1.5;
}

.tier-feedback svg {
  width: 14px;
  flex: 0 0 auto;
}

.tier-feedback.success {
  background: color-mix(in srgb, var(--success) 9%, transparent);
  color: var(--success);
}

.tier-feedback.danger {
  background: color-mix(in srgb, var(--danger) 9%, transparent);
  color: var(--danger);
}

.tier-feedback button {
  margin-left: auto;
  border: 0;
  background: transparent;
  color: inherit;
  font-size: 8px;
  font-weight: 700;
}

.tier-state {
  min-height: 330px;
  padding: 32px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  font-size: 9px;
  text-align: center;
}

.tier-state svg {
  width: 24px;
}

.tier-state b {
  color: var(--text);
}

.tier-grid {
  padding: 14px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.tier-grid article {
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
}

.tier-grid article > header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 9px;
}

.level-mark {
  width: 36px;
  height: 36px;
  border-radius: 7px;
  display: grid;
  place-items: center;
  background: var(--inverse);
  color: var(--inverse-text);
  font-size: 10px;
  font-weight: 800;
}

.tier-grid h3 {
  margin: 0 0 4px;
  overflow: hidden;
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tier-grid header span {
  font-size: 7px;
  font-weight: 700;
}

.tier-grid header span.enabled {
  color: var(--success);
}

.tier-grid header span.disabled {
  color: var(--muted);
}

.discount-value {
  margin: 16px 0 12px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.discount-value span {
  color: var(--muted);
  font-size: 7px;
}

.discount-value strong {
  font-size: 25px;
  letter-spacing: -0.04em;
}

.tier-grid dl {
  margin: 0;
}

.tier-grid dl div {
  padding: 7px 0;
  border-top: 1px solid var(--line);
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 7px;
}

.tier-grid dt {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
}

.tier-grid dt svg {
  width: 11px;
}

.tier-grid dd {
  margin: 0;
  text-align: right;
}

.tier-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 130;
  padding: 24px;
  display: flex;
  justify-content: flex-end;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
}

.tier-modal {
  width: min(520px, 100%);
  height: 100%;
  border: 1px solid var(--line);
  border-radius: 9px;
  overflow-y: auto;
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
}

.tier-modal > header {
  position: sticky;
  top: 0;
  z-index: 2;
  padding: 17px 19px;
  border-bottom: 1px solid var(--line);
  display: flex;
  justify-content: space-between;
  gap: 12px;
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(12px);
}

.tier-modal > header > button {
  width: 31px;
  padding: 0;
}

.tier-modal form {
  padding: 18px 19px;
  display: grid;
  gap: 15px;
}

.tier-modal form > .tier-feedback {
  margin: 0;
}

.tier-modal label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 600;
}

.tier-modal input,
.tier-modal select,
.tier-modal textarea {
  width: 100%;
  min-height: 36px;
  padding: 8px 9px;
  border: 1px solid var(--line);
  border-radius: 5px;
  outline: 0;
  background: var(--surface-2);
  color: var(--text);
  font-size: 9px;
}

.tier-modal textarea {
  min-height: 88px;
  resize: vertical;
  line-height: 1.55;
}

.tier-modal input:focus,
.tier-modal select:focus,
.tier-modal textarea:focus {
  border-color: var(--text);
}

.tier-modal label small {
  color: var(--muted);
  font-size: 7px;
  font-weight: 400;
  line-height: 1.55;
}

.percent-input {
  position: relative;
}

.percent-input input {
  padding-right: 30px;
}

.percent-input span {
  position: absolute;
  right: 10px;
  top: 10px;
  color: var(--muted);
}

.enabled-control {
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  display: grid !important;
  grid-template-columns: auto 1fr;
  align-items: start;
  background: var(--surface-2);
}

.enabled-control input {
  width: 15px;
  min-height: 15px;
  margin: 1px 0 0;
}

.enabled-control > span {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.enabled-control b {
  color: var(--text);
  font-size: 8px;
}

.baseline-note {
  margin: 0;
  padding: 9px 10px;
  border-left: 2px solid var(--warn);
  background: color-mix(in srgb, var(--warn) 7%, transparent);
  color: var(--muted);
  font-size: 7px;
  line-height: 1.6;
}

.tier-modal footer {
  padding-top: 3px;
  display: flex;
  justify-content: flex-end;
  gap: 7px;
}

.spinning {
  animation: tier-spin 0.8s linear infinite;
}

@keyframes tier-spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 980px) {
  .tier-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .tier-header {
    align-items: stretch;
    flex-direction: column;
  }

  .tier-actions button {
    flex: 1;
  }

  .tier-grid {
    grid-template-columns: 1fr;
  }

  .tier-modal-backdrop {
    padding: 0;
  }

  .tier-modal {
    border-radius: 0;
  }
}
</style>
