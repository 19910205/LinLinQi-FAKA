<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowRight,
  Eye,
  EyeOff,
  KeyRound,
  LockKeyhole,
  UserRound,
} from "@lucide/vue";
import { useAuthStore } from "../stores/auth";

const { t } = useI18n();
const account = ref("");
const password = ref("");
const visible = ref(false);
const otp = ref("");
const loading = ref(false);
const error = ref("");
const router = useRouter();
const auth = useAuthStore();

async function submit() {
  loading.value = true;
  error.value = "";
  try {
    await auth.login(account.value, password.value, otp.value);
    router.push("/");
  } catch {
    error.value = t("login.errLogin");
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <main class="login-page">
    <section class="login-aside">
      <div class="aside-grid"></div>
      <div class="login-brand">
        <span class="brand-mark">LQ</span><span>LinLinQi</span
        ><small>{{ t("adminKicker.adminConsole") }}</small>
      </div>
      <div class="aside-copy">
        <span>{{ t("adminKicker.enterpriseCommerce") }}</span>
        <h1>{{ t("login.tagline") }}</h1>
        <p>{{ t("login.subtitle") }}</p>
      </div>
      <div class="aside-stats">
        <div>
          <strong>{{ t("login.statTxn") }}</strong
          ><span>{{ t("login.statTxnDesc") }}</span>
        </div>
        <div>
          <strong>{{ t("login.statAes") }}</strong
          ><span>{{ t("login.statAesDesc") }}</span>
        </div>
        <div>
          <strong>{{ t("login.statRbac") }}</strong
          ><span>{{ t("login.statRbacDesc") }}</span>
        </div>
      </div>
    </section>
    <section class="login-main">
      <div class="login-card">
        <div class="mobile-brand">
          <span class="brand-mark">LQ</span> LinLinQi
        </div>
        <span class="kicker">{{ t("adminKicker.secureAccess") }}</span>
        <h2>{{ t("login.title") }}</h2>
        <p>{{ t("login.subtitleForm") }}</p>
        <form @submit.prevent="submit">
          <label
            >{{ t("login.username") }}
            <div class="field">
              <UserRound :size="17" /><input
                v-model="account"
                autocomplete="username"
                :placeholder="t('login.usernamePlaceholder')"
              /></div></label
          ><label
            >{{ t("login.password") }}
            <div class="field">
              <LockKeyhole :size="17" /><input
                v-model="password"
                :type="visible ? 'text' : 'password'"
                autocomplete="current-password"
                :placeholder="t('login.passwordPlaceholder')"
              /><button
                type="button"
                :aria-label="t('login.showPassword')"
                @click="visible = !visible"
              >
                <EyeOff v-if="visible" :size="17" /><Eye v-else :size="17" />
              </button></div></label
          ><label
            >{{ t("login.otp") }}
            <span class="optional">{{ t("login.otpOptional") }}</span>
            <div class="field">
              <KeyRound :size="17" /><input
                v-model="otp"
                autocomplete="one-time-code"
                maxlength="13"
                :placeholder="t('login.otpPlaceholder')"
              /></div
          ></label>
          <div class="form-line">
            <span>{{ t("login.sessionNote") }}</span
            ><span>{{ t("login.credentialNote") }}</span>
          </div>
          <p v-if="error" class="form-error">{{ error }}</p>
          <button class="primary-button" :disabled="loading">
            {{ loading ? t("login.verifying") : t("login.login") }}
            <ArrowRight :size="16" />
          </button>
        </form>
      </div>
      <footer>© 2026 LinLinQi Cloud · {{ t("login.footer") }}</footer>
    </section>
  </main>
</template>
