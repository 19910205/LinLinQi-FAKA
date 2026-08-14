<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowRight,
  Link2,
  LockKeyhole,
  Mail,
  ShieldCheck,
  UserRound,
} from "@lucide/vue";
import {
  exchangeOAuth,
  fetchOAuthProviders,
  login,
  register,
  requestPasswordReset,
  resetPassword,
  startOAuth,
} from "../api";
import type { OAuthProvider } from "../types";
import { safeInternalPath, safeNavigationURL } from "../utils/publicUrl";

const { t } = useI18n();

const route = useRoute();
const router = useRouter();
const mode = computed(() =>
  route.path === "/auth/oauth/callback"
    ? "oauth-callback"
    : route.path.split("/").pop() || "login",
);
const email = ref("");
const nickname = ref("");
const password = ref("");
const referralCode = ref("");
const loading = ref(false);
const error = ref("");
const notice = ref("");
const oauthProviders = ref<OAuthProvider[]>([]);
const oauthLoading = ref("");
const oauthHandled = ref(false);
const referralQuery = computed(() =>
  referralCode.value.trim()
    ? { ref: referralCode.value.trim().toUpperCase() }
    : {},
);
const registerTarget = computed(() => ({
  path: "/auth/register",
  query: referralQuery.value,
}));
const loginTarget = computed(() => ({
  path: "/auth/login",
  query: referralQuery.value,
}));

watch(
  [mode, () => route.query.ref],
  ([currentMode, value]) => {
    const raw = Array.isArray(value) ? value[0] : value;
    if (raw) {
      referralCode.value = String(raw).trim().toUpperCase().slice(0, 40);
    } else if (currentMode === "register") {
      referralCode.value = "";
    }
  },
  { immediate: true },
);

watch(
  [mode, () => route.query.code, () => route.query.error],
  () => {
    if (mode.value === "oauth-callback") void handleOAuthCallback();
    else if (["login", "register"].includes(mode.value))
      void loadOAuthProviders();
  },
  { immediate: true },
);

async function loadOAuthProviders() {
  try {
    oauthProviders.value = await fetchOAuthProviders();
  } catch {
    oauthProviders.value = [];
  }
}

async function beginOAuth(provider: OAuthProvider) {
  error.value = "";
  notice.value = "";
  oauthLoading.value = provider.code;
  try {
    const requestedRedirect = safeInternalPath(
      String(route.query.redirect || ""),
      "/account/profile",
    );
    const result = await startOAuth(provider.code, requestedRedirect);
    const target = safeNavigationURL(result.auth_url);
    if (!target) throw new Error("unsafe authorization URL");
    location.assign(target);
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("auth.oauthErrProvider");
    oauthLoading.value = "";
  }
}

async function handleOAuthCallback() {
  if (oauthHandled.value) return;
  oauthHandled.value = true;
  const callbackError = String(route.query.error || "");
  const code = String(route.query.code || "");
  if (callbackError || !code) {
    error.value = t("auth.oauthErrFailed");
    return;
  }
  loading.value = true;
  try {
    const result = await exchangeOAuth(code);
    await router.replace(safeInternalPath(result.redirect, "/account/profile"));
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("auth.oauthErrToken");
  } finally {
    loading.value = false;
  }
}

watch(
  () => route.query.notice,
  (value) => {
    if (value === "password-changed") {
      notice.value = t("account.passwordChangedLogin");
    }
  },
  { immediate: true },
);

async function submit() {
  error.value = "";
  notice.value = "";
  if (mode.value !== "reset" && !/^\S+@\S+\.\S+$/.test(email.value)) {
    error.value = t("auth.errEmail");
    return;
  }
  if (mode.value === "forgot") {
    loading.value = true;
    try {
      await requestPasswordReset(email.value);
      notice.value = t("auth.forgotSent");
    } catch (reason: any) {
      error.value = reason?.response?.data?.message || t("auth.errReset");
    } finally {
      loading.value = false;
    }
    return;
  }
  if (password.value.length < 8) {
    error.value = t("auth.errPassword");
    return;
  }
  const referral = referralCode.value.trim().toUpperCase();
  if (
    mode.value === "register" &&
    referral &&
    !/^[A-Z0-9]{3,40}$/.test(referral)
  ) {
    error.value = t("auth.errReferral");
    return;
  }
  loading.value = true;
  try {
    if (mode.value === "reset") {
      const token = String(route.query.token || "");
      if (!token) throw new Error("missing token");
      await resetPassword(token, password.value);
      notice.value = t("auth.resetDone");
      return;
    }
    if (mode.value === "register")
      await register(
        email.value,
        password.value,
        nickname.value || email.value.split("@")[0],
        referral,
      );
    else await login(email.value, password.value);
    const redirect = String(route.query.redirect || "");
    await router.push(
      redirect.startsWith("/") && !redirect.startsWith("//")
        ? redirect
        : "/account/profile",
    );
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("auth.errOperation");
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <section class="account-page section">
    <div class="auth-card">
      <span class="brand-mark large">LQ</span
      ><span class="kicker">{{ t("kicker.memberAccess") }}</span>
      <h1>
        {{
          mode === "register"
            ? t("auth.createAccount")
            : mode === "forgot"
              ? t("auth.forgotPassword")
              : mode === "reset"
                ? t("auth.resetPassword")
                : mode === "oauth-callback"
                  ? t("auth.oauthCompleting")
                  : t("auth.welcomeBack")
        }}
      </h1>
      <p>
        {{
          mode === "register"
            ? t("auth.createDesc")
            : mode === "forgot"
              ? t("auth.forgotDesc")
              : mode === "reset"
                ? t("auth.resetDesc")
                : mode === "oauth-callback"
                  ? t("auth.oauthVerifying")
                  : t("auth.loginDesc")
        }}
      </p>
      <label v-if="mode === 'register'"
        ><span>{{ t("auth.nickname") }}</span>
        <div class="input-icon">
          <UserRound :size="17" /><input
            v-model="nickname"
            maxlength="80"
            :placeholder="t('auth.nicknamePlaceholder')"
          /></div></label
      ><label v-if="!['reset', 'oauth-callback'].includes(mode)"
        ><span>{{ t("auth.email") }}</span>
        <div class="input-icon">
          <Mail :size="17" /><input
            v-model="email"
            type="email"
            autocomplete="email"
            placeholder="name@example.com"
          /></div></label
      ><label v-if="mode === 'register'"
        ><span>{{ t("auth.referral") }}</span>
        <div class="input-icon">
          <Link2 :size="17" /><input
            v-model="referralCode"
            maxlength="40"
            autocomplete="off"
            autocapitalize="characters"
            spellcheck="false"
            :placeholder="t('auth.referralPlaceholder')"
          />
        </div>
        <small class="referral-hint">{{ t("auth.referralHint") }}</small></label
      ><label v-if="!['forgot', 'oauth-callback'].includes(mode)"
        ><span>{{ t("auth.password") }}</span>
        <div class="input-icon">
          <LockKeyhole :size="17" /><input
            v-model="password"
            type="password"
            :autocomplete="
              ['register', 'reset'].includes(mode)
                ? 'new-password'
                : 'current-password'
            "
            :placeholder="t('auth.passwordPlaceholder')"
            @keyup.enter="submit"
          /></div
      ></label>
      <p v-if="error" class="form-error">{{ error }}</p>
      <p v-if="notice" class="form-notice">{{ notice }}</p>
      <div
        v-if="mode === 'oauth-callback' && loading"
        class="oauth-callback-progress"
      >
        <span></span><b>{{ t("auth.oauthProgress") }}</b>
      </div>
      <RouterLink
        v-if="mode === 'oauth-callback' && error"
        class="button secondary wide"
        to="/auth/login"
        >{{ t("auth.oauthBack") }}</RouterLink
      >
      <button
        v-if="mode !== 'oauth-callback'"
        class="button primary wide"
        :disabled="loading"
        @click="submit"
      >
        {{
          loading
            ? t("auth.submitting")
            : mode === "register"
              ? t("auth.register")
              : mode === "forgot"
                ? t("auth.sendReset")
                : mode === "reset"
                  ? t("auth.confirmReset")
                  : t("auth.login")
        }}
        <ArrowRight :size="16" />
      </button>
      <div
        v-if="['login', 'register'].includes(mode) && oauthProviders.length"
        class="oauth-provider-list"
      >
        <small>{{ t("auth.oauthOr") }}</small>
        <button
          v-for="provider in oauthProviders"
          :key="provider.code"
          type="button"
          class="button secondary wide"
          :disabled="Boolean(oauthLoading)"
          @click="beginOAuth(provider)"
        >
          <ShieldCheck :size="16" />
          {{
            oauthLoading === provider.code
              ? t("auth.oauthConnecting")
              : t("auth.oauthContinueWith", { provider: provider.name })
          }}
        </button>
      </div>
      <div v-if="mode !== 'oauth-callback'" class="auth-divider">
        <span>{{ t("auth.or") }}</span>
      </div>
      <RouterLink
        v-if="mode === 'login'"
        class="button secondary wide"
        :to="registerTarget"
        >{{ t("auth.createNew") }}</RouterLink
      ><RouterLink
        v-else-if="mode !== 'oauth-callback'"
        class="button secondary wide"
        :to="loginTarget"
        >{{ t("auth.backToLogin") }}</RouterLink
      ><RouterLink
        v-if="mode === 'login'"
        class="auth-text-link"
        to="/auth/forgot"
        >{{ t("auth.forgot") }}</RouterLink
      ><small v-if="mode !== 'oauth-callback'">{{ t("auth.agree") }}</small>
    </div>
  </section>
</template>
