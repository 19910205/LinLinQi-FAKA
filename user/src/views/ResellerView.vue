<script setup lang="ts">
import { computed, ref } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowRight,
  Boxes,
  CircleDollarSign,
  ShieldCheck,
  Store,
} from "@lucide/vue";
import ResellerConsole from "../components/ResellerConsole.vue";
import { applyReseller } from "../api";
const { t } = useI18n();
const route = useRoute();
const section = computed(() => String(route.params.section || "apply"));
const businessName = ref("");
const submitting = ref(false);
const error = ref("");
const success = ref("");
async function submit() {
  error.value = "";
  success.value = "";
  const name = businessName.value.trim();
  if (Array.from(name).length < 2 || Array.from(name).length > 160) {
    error.value = t("reseller.errName");
    return;
  }
  submitting.value = true;
  try {
    await applyReseller(name);
    businessName.value = "";
    success.value = t("reseller.submitted");
  } catch (reason: any) {
    error.value = reason?.response?.data?.message || t("reseller.errSubmit");
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <section v-if="section === 'apply'" class="reseller-apply">
    <div class="reseller-hero">
      <div class="container reseller-hero-layout">
        <div class="reseller-hero-copy">
          <span class="kicker">{{ t("kicker.linlinqiChannel") }}</span>
          <h1>{{ t("reseller.heroTitle") }}</h1>
          <p>{{ t("reseller.heroSub") }}</p>
          <div class="reseller-hero-actions">
            <a class="button primary" href="#apply"
              >{{ t("reseller.applyNow") }} <ArrowRight /></a
            ><RouterLink class="button secondary" to="/reseller/dashboard">{{
              t("reseller.enterConsole")
            }}</RouterLink>
          </div>
        </div>
        <img
          class="reseller-hero-art"
          src="/assets/brand/linlinqi-hero-reseller.webp"
          alt=""
          decoding="async"
        />
      </div>
    </div>
    <div class="container reseller-features">
      <article>
        <Store />
        <h3>{{ t("reseller.featBrand") }}</h3>
        <p>{{ t("reseller.featBrandDesc") }}</p>
      </article>
      <article>
        <Boxes />
        <h3>{{ t("reseller.featSupply") }}</h3>
        <p>{{ t("reseller.featSupplyDesc") }}</p>
      </article>
      <article>
        <CircleDollarSign />
        <h3>{{ t("reseller.featLedger") }}</h3>
        <p>{{ t("reseller.featLedgerDesc") }}</p>
      </article>
      <article>
        <ShieldCheck />
        <h3>{{ t("reseller.featSecurity") }}</h3>
        <p>{{ t("reseller.featSecurityDesc") }}</p>
      </article>
    </div>
    <div id="apply" class="container apply-card">
      <div>
        <span class="kicker">{{ t("kicker.application") }}</span>
        <h2>{{ t("reseller.applyTitle") }}</h2>
        <p>{{ t("reseller.applySub") }}</p>
        <ul>
          <li>{{ t("reseller.applyReq1") }}</li>
          <li>{{ t("reseller.applyReq2") }}</li>
          <li>{{ t("reseller.applyReq3") }}</li>
        </ul>
      </div>
      <form @submit.prevent="submit">
        <label
          >{{ t("reseller.businessName")
          }}<input
            v-model="businessName"
            maxlength="160"
            :placeholder="t('reseller.businessPlaceholder')"
        /></label>
        <p v-if="error" class="form-error">{{ error }}</p>
        <p v-if="success" class="form-notice">{{ success }}</p>
        <button class="button primary wide" :disabled="submitting">
          {{ submitting ? t("reseller.submitting") : t("reseller.submit") }}
        </button>
      </form>
    </div>
  </section>
  <ResellerConsole v-else :section="section" />
</template>
