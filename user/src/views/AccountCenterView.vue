<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  Activity,
  BadgeDollarSign,
  BookKey,
  ChevronRight,
  CircleHelp,
  Copy,
  Gift,
  LockKeyhole,
  LogOut,
  PackageCheck,
  Plus,
  Save,
  ShieldCheck,
  Trash2,
  UserRound,
  WalletCards,
  Webhook,
  Bell,
} from "@lucide/vue";
import AffiliateCenter from "../components/AffiliateCenter.vue";
import GiftCardCenter from "../components/GiftCardCenter.vue";
import TicketCenter from "../components/TicketCenter.vue";
import WalletCenter from "../components/WalletCenter.vue";
import UserNotificationCenter from "../components/UserNotificationCenter.vue";
import {
  changeMyPassword,
  createAPICredential,
  createWebhook,
  deleteWebhook,
  fetchAccountResource,
  fetchWebhooks,
  logout,
  revokeAPICredential,
  revokeSession,
  updateMyProfile,
  uploadMyAvatar,
} from "../api";
import { formatMinor } from "../utils/money";
import { safePublicHTTPURL } from "../utils/publicUrl";
const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const section = computed(() => String(route.params.section || "profile"));
const nav = computed(
  () =>
    [
      ["profile", t("account.profile"), UserRound],
      ["orders", t("account.orders"), PackageCheck],
      ["wallet", t("account.wallet"), WalletCards],
      ["notifications", t("account.notifications"), Bell],
      ["gift-cards", t("account.giftCards"), Gift],
      ["tickets", t("account.tickets"), CircleHelp],
      ["affiliate", t("account.affiliate"), BadgeDollarSign],
      ["api", t("account.api"), BookKey],
      ["webhooks", t("account.webhooks"), Webhook],
      ["security", t("account.security"), ShieldCheck],
    ] as const,
);
const titles = computed<Record<string, [string, string]>>(() => ({
  profile: [t("account.profile"), t("account.titles.profile")],
  orders: [t("account.orders"), t("account.titles.orders")],
  wallet: [t("account.wallet"), t("account.titles.wallet")],
  notifications: [
    t("account.notifications"),
    t("account.titles.notifications"),
  ],
  ["gift-cards"]: [t("account.giftCards"), t("account.titles.giftCards")],
  tickets: [t("account.tickets"), t("account.titles.tickets")],
  affiliate: [t("account.affiliate"), t("account.titles.affiliate")],
  api: [t("account.api"), t("account.titles.api")],
  webhooks: [t("account.webhooks"), t("account.titles.webhooks")],
  security: [t("account.security"), t("account.titles.security")],
}));
const current = computed(
  () => titles.value[section.value] || titles.value.profile,
);
const profile = ref<any>(null);
const profileAvatarURL = computed(() =>
  safePublicHTTPURL(profile.value?.avatar_url),
);
const data = ref<any>(null);
const loading = ref(false);
const error = ref("");
const oneTimeSecret = ref("");
const credentialName = ref("");
const credentialCreateOpen = ref(false);
const oneTimeWebhookSecret = ref("");
const webhookURL = ref("");
const mutating = ref(false);
const actionNotice = ref("");
const copied = ref("");
const nickname = ref("");
const email = ref("");
const emailCurrentPassword = ref("");
const profileSaving = ref(false);
const avatarSaving = ref(false);
const currentPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const passwordSaving = ref(false);
let copyTimer: ReturnType<typeof setTimeout> | undefined;
const money = (value: number, currency?: string) =>
  formatMinor(value, currency, locale);
const date = (value: string) =>
  value ? new Date(value).toLocaleString(locale.value, { hour12: false }) : "—";
const resourceItems = computed<any[]>(() =>
  Array.isArray(data.value) ? data.value : data.value?.items || [],
);
const activeCredentialCount = computed(
  () => resourceItems.value.filter((item) => item.status !== "revoked").length,
);
function webhookEvents(value: string | string[]) {
  if (Array.isArray(value)) return value.join("、");
  try {
    const events = JSON.parse(value);
    return Array.isArray(events) ? events.join("、") : String(value || "—");
  } catch {
    return value || "—";
  }
}
async function load() {
  loading.value = true;
  error.value = "";
  oneTimeSecret.value = "";
  oneTimeWebhookSecret.value = "";
  actionNotice.value = "";
  try {
    let accountData: any = null;
    if (!profile.value || section.value === "profile") {
      accountData = await fetchAccountResource();
      profile.value = accountData.user;
    }
    const resource: Record<string, string> = {
      orders: "orders",
      wallet: "wallet",
      api: "api-credentials",
      webhooks: "webhooks",
      security: "sessions",
    };
    data.value =
      section.value === "profile"
        ? accountData
        : [
              "tickets",
              "gift-cards",
              "affiliate",
              "wallet",
              "notifications",
            ].includes(section.value)
          ? null
          : section.value === "webhooks"
            ? await fetchWebhooks()
            : await fetchAccountResource(resource[section.value] || "");
    if (section.value === "profile" && data.value?.user) {
      nickname.value = data.value.user.nickname || "";
      email.value = data.value.user.email || "";
      emailCurrentPassword.value = "";
    }
  } catch (reason: any) {
    data.value = null;
    error.value = reason?.response?.data?.message || t("account.errLoad");
  } finally {
    loading.value = false;
  }
}
async function saveProfile() {
  error.value = "";
  actionNotice.value = "";
  const value = nickname.value.trim();
  const normalizedEmail = email.value.trim().toLowerCase();
  if ([...value].length < 2 || [...value].length > 80) {
    error.value = t("account.errProfile");
    return;
  }
  if (
    normalizedEmail.length > 190 ||
    !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)
  ) {
    error.value = t("account.errEmailInvalid");
    return;
  }
  const changingEmail =
    normalizedEmail !== String(data.value?.user?.email || "").toLowerCase();
  if (changingEmail && !emailCurrentPassword.value) {
    error.value = t("account.errEmailPassword");
    return;
  }
  profileSaving.value = true;
  try {
    const user = await updateMyProfile(
      value,
      normalizedEmail,
      changingEmail ? emailCurrentPassword.value : "",
    );
    profile.value = user;
    if (data.value) data.value.user = user;
    nickname.value = user.nickname;
    email.value = user.email;
    emailCurrentPassword.value = "";
    const stored = localStorage.getItem("linlinqi-user-profile");
    if (stored) {
      try {
        localStorage.setItem(
          "linlinqi-user-profile",
          JSON.stringify({
            ...JSON.parse(stored),
            nickname: user.nickname,
            email: user.email,
          }),
        );
      } catch {
        localStorage.removeItem("linlinqi-user-profile");
      }
    }
    actionNotice.value = t("account.profileSaved");
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("account.errProfile");
  } finally {
    profileSaving.value = false;
  }
}
async function changeAvatar(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  if (
    !/^image\/(png|jpeg|webp|gif)$/.test(file.type) ||
    file.size > 5 * 1024 * 1024
  ) {
    error.value = t("account.avatarFileInvalid");
    return;
  }
  avatarSaving.value = true;
  error.value = "";
  try {
    const user = await uploadMyAvatar(file);
    profile.value = user;
    if (data.value) data.value.user = user;
    const stored = localStorage.getItem("linlinqi-user-profile");
    if (stored)
      localStorage.setItem(
        "linlinqi-user-profile",
        JSON.stringify({ ...JSON.parse(stored), avatar_url: user.avatar_url }),
      );
    actionNotice.value = t("account.avatarUpdated");
  } catch (reason: any) {
    error.value =
      reason?.response?.data?.message || t("account.avatarUploadFailed");
  } finally {
    avatarSaving.value = false;
  }
}
async function submitPasswordChange() {
  error.value = "";
  actionNotice.value = "";
  if (
    newPassword.value !== confirmPassword.value ||
    [...newPassword.value].length < 8 ||
    new TextEncoder().encode(newPassword.value).length > 72
  ) {
    error.value = t("account.errPasswordMismatch");
    return;
  }
  passwordSaving.value = true;
  try {
    await changeMyPassword(currentPassword.value, newPassword.value);
    currentPassword.value = "";
    newPassword.value = "";
    confirmPassword.value = "";
    try {
      await logout();
    } finally {
      await router.push({
        path: "/auth/login",
        query: { notice: "password-changed" },
      });
    }
  } catch (reason: any) {
    error.value =
      reason?.response?.data?.message || t("account.errPasswordChange");
  } finally {
    passwordSaving.value = false;
  }
}
async function addWebhook() {
  error.value = "";
  actionNotice.value = "";
  let target: URL;
  try {
    target = new URL(webhookURL.value.trim());
  } catch {
    error.value = t("account.errWebhookURL");
    return;
  }
  if (target.protocol !== "https:" || target.username || target.password) {
    error.value = t("account.errWebhookHTTPS");
    return;
  }
  mutating.value = true;
  try {
    const result = await createWebhook(target.toString());
    if (!result.secret) throw new Error("missing webhook secret");
    await load();
    webhookURL.value = "";
    oneTimeWebhookSecret.value = result.secret;
    actionNotice.value = t("account.webhookCreated");
  } catch (reason: any) {
    error.value =
      reason?.response?.data?.message || t("account.errWebhookCreate");
  } finally {
    mutating.value = false;
  }
}
async function removeWebhook(id: string) {
  if (!window.confirm(t("account.confirmWebhookDelete"))) return;
  error.value = "";
  actionNotice.value = "";
  mutating.value = true;
  try {
    await deleteWebhook(id);
    await load();
    actionNotice.value = t("account.webhookDeleted");
  } catch (reason: any) {
    error.value =
      reason?.response?.data?.message || t("account.errWebhookDelete");
  } finally {
    mutating.value = false;
  }
}
async function copySensitive(value: string, key: string) {
  error.value = "";
  if (!value || !window.isSecureContext || !navigator.clipboard) {
    error.value = t("account.errClipboard");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    copied.value = key;
    if (copyTimer) clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copied.value = "";
    }, 2500);
  } catch {
    error.value = t("account.errCopy");
  }
}
async function createKey() {
  error.value = "";
  actionNotice.value = "";
  const name = credentialName.value.trim().replace(/\s+/g, " ");
  if (
    [...name].length < 2 ||
    [...name].length > 100 ||
    /[\u0000-\u001f\u007f-\u009f]/.test(credentialName.value)
  ) {
    error.value = t("account.errCredentialName");
    return;
  }
  mutating.value = true;
  try {
    const result = await createAPICredential(name);
    oneTimeSecret.value = result.secret;
    await load();
    oneTimeSecret.value = result.secret;
    credentialName.value = "";
    credentialCreateOpen.value = false;
    actionNotice.value = t("account.credentialSubmitted");
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("account.errCredential");
  } finally {
    mutating.value = false;
  }
}
async function revokeCredential(id: string, name: string) {
  if (!window.confirm(t("account.confirmRevoke", { name }))) return;
  error.value = "";
  actionNotice.value = "";
  mutating.value = true;
  try {
    await revokeAPICredential(id);
    await load();
    actionNotice.value = t("account.credentialRevoked", { name });
  } catch (reason: any) {
    error.value =
      reason?.response?.data?.message || t("account.errRevokeCredential");
  } finally {
    mutating.value = false;
  }
}
function credentialStatus(status: string) {
  return (
    {
      pending: t("account.credStatusPending"),
      active: t("account.credStatusActive"),
      suspended: t("account.credStatusSuspended"),
      revoked: t("account.credStatusRevoked"),
    }[status] || status
  );
}
async function revokeUserSession(id: string) {
  try {
    await revokeSession(id);
    await load();
  } catch {
    error.value = t("account.errRevoke");
  }
}
async function signOut() {
  await logout();
  await router.push("/auth/login");
}
watch(section, load, { immediate: true });
</script>

<template>
  <section class="account-center">
    <div class="container account-grid">
      <aside class="account-sidebar">
        <div class="member-card">
          <label class="avatar-picker" :title="t('account.avatarChange')">
            <img
              v-if="profileAvatarURL"
              :src="profileAvatarURL"
              :alt="t('account.avatarAlt')"
              referrerpolicy="no-referrer"
            />
            <span v-else>LQ</span>
            <input
              type="file"
              accept="image/png,image/jpeg,image/webp,image/gif"
              @change="changeAvatar"
            />
          </label>
          <div>
            <b>{{ profile?.nickname || t("account.defaultUser") }}</b
            ><small>{{ profile?.email || t("account.notSignedIn") }}</small>
          </div>
          <em>{{ data?.member_level?.name || t("account.memberAccount") }}</em>
        </div>
        <nav>
          <RouterLink
            v-for="item in nav"
            :key="item[0]"
            :to="`/account/${item[0]}`"
            ><component :is="item[2]" /><span>{{ item[1] }}</span
            ><ChevronRight
          /></RouterLink>
        </nav>
        <RouterLink class="reseller-entry" to="/reseller/dashboard"
          ><Activity />
          <div>
            <b>{{ t("account.resellerConsole") }}</b
            ><span>{{ t("account.resellerConsoleDesc") }}</span>
          </div></RouterLink
        ><button @click="signOut"><LogOut />{{ t("account.signOut") }}</button>
      </aside>
      <main class="account-content">
        <header>
          <div>
            <span class="kicker">{{ t("kicker.memberCenter") }}</span>
            <h1>{{ current[0] }}</h1>
            <p>{{ current[1] }}</p>
          </div>
          <button
            v-if="section === 'api'"
            class="button primary"
            :disabled="activeCredentialCount >= 10 || mutating"
            @click="credentialCreateOpen = !credentialCreateOpen"
          >
            <Plus />{{ t("account.createCredential") }}
          </button>
        </header>
        <p v-if="error" class="form-error">{{ error }}</p>
        <p v-if="actionNotice" class="form-notice" role="status">
          {{ actionNotice }}
        </p>
        <p v-if="loading">{{ t("account.loading") }}</p>
        <template v-else-if="section === 'profile' && data"
          ><div class="profile-overview">
            <article>
              <span>{{ t("account.accountStatus") }}</span
              ><strong>{{ data.user.status }}</strong
              ><small>{{
                t("account.lastLogin", { time: date(data.user.last_login_at) })
              }}</small>
            </article>
            <article>
              <span>{{ t("account.memberLevel") }}</span
              ><strong>{{
                data.member_level?.name || t("account.standardMember")
              }}</strong
              ><small>{{ t("account.memberDiscount") }}</small>
            </article>
            <article>
              <span>{{ t("account.accountCreated") }}</span
              ><strong>{{ date(data.user.created_at).split(" ")[0] }}</strong
              ><small>{{
                t("account.userId", { id: data.user.id.slice(0, 8) })
              }}</small>
            </article>
          </div>
          <section class="account-panel form">
            <h2>{{ t("account.basicInfo") }}</h2>
            <p class="avatar-hint">
              {{ t("account.avatarHint") }}
            </p>
            <div class="form-grid">
              <label
                >{{ t("account.nickname")
                }}<input
                  v-model="nickname"
                  minlength="2"
                  maxlength="80"
                  autocomplete="nickname"
                /><small>{{ t("account.nicknameHint") }}</small></label
              ><label
                >{{ t("account.email")
                }}<input
                  v-model="email"
                  type="email"
                  maxlength="190"
                  autocomplete="email"
                /><small>{{ t("account.emailManaged") }}</small></label
              ><label
                v-if="
                  email.trim().toLowerCase() !== data.user.email.toLowerCase()
                "
                >{{ t("account.emailCurrentPassword")
                }}<input
                  v-model="emailCurrentPassword"
                  type="password"
                  maxlength="72"
                  autocomplete="current-password"
                /><small>{{
                  t("account.emailCurrentPasswordHint")
                }}</small></label
              >
            </div>
            <button
              class="button primary"
              :disabled="profileSaving"
              @click="saveProfile"
            >
              <Save />
              {{
                profileSaving ? t("account.saving") : t("account.saveProfile")
              }}
            </button>
          </section></template
        ><template v-else-if="section === 'orders'"
          ><section class="account-panel">
            <div class="mobile-orders">
              <article v-for="order in data?.items || []" :key="order.id">
                <div>
                  <span>{{ order.order_no }}</span
                  ><b>{{
                    order.items
                      ?.map((item: any) => item.product_name)
                      .join("、") || t("account.digitalGoods")
                  }}</b>
                </div>
                <strong>{{ money(order.total, order.currency) }}</strong
                ><em>{{ order.status }}</em>
                <div
                  v-if="order.items?.some((item: any) => item.card_content)"
                  class="order-delivery"
                >
                  <div
                    v-for="item in order.items.filter(
                      (entry: any) => entry.card_content,
                    )"
                    :key="item.id"
                    class="order-delivery-item"
                  >
                    <span>
                      <b>{{ item.product_name }}</b>
                      <small>{{
                        t("account.delivered", { n: item.quantity })
                      }}</small>
                    </span>
                    <pre>{{ item.card_content }}</pre>
                    <button
                      type="button"
                      :aria-label="
                        t('account.copyCardAria', { name: item.product_name })
                      "
                      @click="
                        copySensitive(
                          item.card_content,
                          `order-${order.id}-${item.id}`,
                        )
                      "
                    >
                      <Copy />
                      {{
                        copied === `order-${order.id}-${item.id}`
                          ? t("account.copied")
                          : t("account.copyCard")
                      }}
                    </button>
                  </div>
                </div>
              </article>
              <p v-if="!data?.items?.length">{{ t("account.noOrders") }}</p>
            </div>
          </section></template
        ><template v-else-if="section === 'notifications'"
          ><UserNotificationCenter /></template
        ><template v-else-if="section === 'wallet'"><WalletCenter /></template
        ><template v-else-if="section === 'gift-cards'"
          ><GiftCardCenter /></template
        ><template v-else-if="section === 'tickets'"><TicketCenter /></template
        ><template v-else-if="section === 'affiliate'"
          ><AffiliateCenter /></template
        ><template v-else-if="section === 'api'"
          ><section
            v-if="credentialCreateOpen"
            class="account-panel credential-create"
          >
            <div class="panel-heading">
              <div>
                <h2>{{ t("account.createCredentialTitle") }}</h2>
                <p>
                  {{ t("account.credentialIntro") }}
                </p>
              </div>
            </div>
            <form @submit.prevent="createKey">
              <label for="credential-name">{{
                t("account.credentialNameLabel")
              }}</label>
              <div>
                <input
                  id="credential-name"
                  v-model="credentialName"
                  maxlength="100"
                  autocomplete="off"
                  :placeholder="t('account.credentialNamePlaceholder')"
                  :disabled="mutating"
                  required
                />
                <button
                  class="button primary"
                  type="submit"
                  :disabled="mutating || activeCredentialCount >= 10"
                >
                  <Plus />{{
                    mutating
                      ? t("account.credentialSubmitting")
                      : t("account.credentialSubmit")
                  }}
                </button>
              </div>
              <small>{{ t("account.credentialQuotaHint") }}</small>
            </form>
          </section>
          <div v-if="oneTimeSecret" class="secret-reveal" role="alert">
            <span>
              <b>{{ t("account.saveSecret") }}</b>
              <small>{{ t("account.saveSecretHint") }}</small>
            </span>
            <code>{{ oneTimeSecret }}</code>
            <button
              type="button"
              @click="copySensitive(oneTimeSecret, 'api-secret')"
            >
              <Copy />{{
                copied === "api-secret"
                  ? t("account.copied")
                  : t("account.copy")
              }}
            </button>
          </div>
          <section class="account-panel">
            <div
              v-for="credential in resourceItems"
              :key="credential.id"
              :class="[
                'api-key-row',
                { revoked: credential.status === 'revoked' },
              ]"
            >
              <BookKey />
              <div class="api-key-main">
                <b>{{ credential.name }}</b
                ><code>{{ credential.key }}</code>
                <small
                  >{{
                    t("account.credentialCreatedAt", {
                      time: date(credential.created_at),
                    })
                  }}
                  {{ date(credential.last_used_at) }}</small
                >
              </div>
              <div class="api-key-scopes">
                <span
                  v-for="permission in credential.permissions.split(',')"
                  :key="permission"
                  >{{ permission.trim() }}</span
                >
              </div>
              <em :class="credential.status">{{
                credentialStatus(credential.status)
              }}</em>
              <button
                v-if="credential.status !== 'revoked'"
                type="button"
                :disabled="mutating"
                @click="revokeCredential(credential.id, credential.name)"
              >
                <Trash2 />{{ t("account.credentialRevoke") }}
              </button>
            </div>
            <p v-if="!resourceItems.length">
              {{ t("account.noCredentials") }}
            </p>
            <p v-else class="credential-limit-note">
              {{
                t("account.credentialCount", { count: activeCredentialCount })
              }}
            </p>
          </section></template
        ><template v-else-if="section === 'webhooks'"
          ><section class="account-panel webhook-create">
            <div class="panel-heading">
              <div>
                <h2>{{ t("account.addWebhook") }}</h2>
                <p>
                  {{
                    t("account.addWebhookDesc", { event: "order.delivered" })
                  }}
                </p>
              </div>
            </div>
            <form @submit.prevent="addWebhook">
              <label for="webhook-url">{{ t("account.webhookURL") }}</label>
              <div>
                <input
                  id="webhook-url"
                  v-model.trim="webhookURL"
                  type="url"
                  inputmode="url"
                  autocomplete="off"
                  spellcheck="false"
                  maxlength="500"
                  placeholder="https://example.com/webhooks/linlinqi"
                  required
                />
                <button class="button primary" :disabled="mutating">
                  <Plus />{{
                    mutating ? t("account.processing") : t("account.add")
                  }}
                </button>
              </div>
              <small>{{ t("account.webhookHint") }}</small>
            </form>
          </section>
          <div v-if="oneTimeWebhookSecret" class="secret-reveal" role="alert">
            <span>
              <b>{{ t("account.webhookSecret") }}</b>
              <small>{{ t("account.webhookSecretHint") }}</small>
            </span>
            <code>{{ oneTimeWebhookSecret }}</code>
            <button
              type="button"
              @click="copySensitive(oneTimeWebhookSecret, 'webhook-secret')"
            >
              <Copy />
              {{
                copied === "webhook-secret"
                  ? t("account.copied")
                  : t("account.copy")
              }}
            </button>
          </div>
          <section class="account-panel webhook-list">
            <h2>{{ t("account.configuredEndpoints") }}</h2>
            <div
              v-for="endpoint in resourceItems"
              :key="endpoint.id"
              class="api-key-row webhook-row"
            >
              <Webhook />
              <div>
                <b>{{ endpoint.url }}</b>
                <code>
                  {{ webhookEvents(endpoint.events) }} ·
                  {{
                    t("account.createdAt", { time: date(endpoint.created_at) })
                  }}
                </code>
                <small v-if="endpoint.failure_count">
                  {{ t("account.failureCount", { n: endpoint.failure_count }) }}
                </small>
              </div>
              <em :class="{ disabled: !endpoint.enabled }">
                {{
                  endpoint.enabled
                    ? t("account.delivering")
                    : t("account.disabled")
                }}
              </em>
              <button
                type="button"
                :disabled="mutating"
                :aria-label="
                  t('account.deleteWebhookAria', { url: endpoint.url })
                "
                @click="removeWebhook(endpoint.id)"
              >
                <Trash2 />{{ t("account.delete") }}
              </button>
            </div>
            <p v-if="!resourceItems.length">
              {{ t("account.noWebhooks") }}
            </p>
          </section></template
        ><template v-else>
          <form
            class="account-panel form password-change"
            @submit.prevent="submitPasswordChange"
          >
            <div class="panel-heading password-heading">
              <div>
                <h2>{{ t("account.passwordTitle") }}</h2>
                <p>{{ t("account.passwordHint") }}</p>
              </div>
              <LockKeyhole />
            </div>
            <div class="form-grid security-password-grid">
              <label
                >{{ t("account.currentPassword")
                }}<input
                  v-model="currentPassword"
                  type="password"
                  autocomplete="current-password"
                  maxlength="72"
                  required
              /></label>
              <label
                >{{ t("account.newPassword")
                }}<input
                  v-model="newPassword"
                  type="password"
                  autocomplete="new-password"
                  maxlength="72"
                  required
              /></label>
              <label
                >{{ t("account.confirmPassword")
                }}<input
                  v-model="confirmPassword"
                  type="password"
                  autocomplete="new-password"
                  maxlength="72"
                  required
              /></label>
            </div>
            <button class="button primary" :disabled="passwordSaving">
              <LockKeyhole />
              {{
                passwordSaving
                  ? t("account.changingPassword")
                  : t("account.changePassword")
              }}
            </button>
          </form>
          <section class="account-panel security-list">
            <h2>{{ t("account.activeSessions") }}</h2>
            <div v-for="session in data || []" :key="session.id">
              <ShieldCheck /><span
                ><b>{{ session.device || t("account.unnamedDevice") }}</b
                ><small
                  >{{ session.ip }} · {{ date(session.last_active_at) }}</small
                ></span
              ><em>{{
                t("account.validUntil", { time: date(session.expires_at) })
              }}</em
              ><button @click="revokeUserSession(session.id)">
                {{ t("account.revoke") }}
              </button>
            </div>
            <p v-if="!data?.length">{{ t("account.noSessions") }}</p>
          </section>
        </template>
      </main>
    </div>
  </section>
</template>
