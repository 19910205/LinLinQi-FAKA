<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Copy, Eye, EyeOff, PackageCheck, Search } from "@lucide/vue";
import { fetchStoreConfig, queryOrder, recentOrderLookups } from "../api";
import type { Order } from "../types";

const { t } = useI18n();

const orderNo = ref("");
const lookupToken = ref("");
const showToken = ref(false);
const loading = ref(false);
const error = ref("");
const order = ref<Order | null>(null);
const supportEmail = ref("");
const copyCard = (value: string) => window.navigator.clipboard.writeText(value);

async function lookup() {
  error.value = "";
  order.value = null;
  if (!orderNo.value || !lookupToken.value) {
    error.value = t("lookup.errFill");
    return;
  }
  loading.value = true;
  try {
    order.value = await queryOrder(orderNo.value, lookupToken.value);
  } catch {
    error.value = t("lookup.errNotFound");
  } finally {
    loading.value = false;
  }
}
onMounted(async () => {
  const recent = recentOrderLookups()[0];
  if (recent) {
    orderNo.value = recent.order_no;
    lookupToken.value = recent.lookup_token;
  }
  try {
    const config = await fetchStoreConfig();
    supportEmail.value = String(config.support_email || "");
  } catch {
    supportEmail.value = "";
  }
});
</script>

<template>
  <section class="lookup-page section">
    <div class="container narrow">
      <span class="kicker">{{ t("kicker.orderLookup") }}</span>
      <h1>{{ t("lookup.title") }}</h1>
      <p>{{ t("lookup.subtitle") }}</p>
      <div class="lookup-card">
        <label
          >{{ t("lookup.orderNo")
          }}<input
            v-model.trim="orderNo"
            :placeholder="t('lookup.orderNoPlaceholder')"
            @keyup.enter="lookup" /></label
        ><label class="lookup-token-field"
          >{{ t("lookup.lookupToken")
          }}<span
            ><input
              v-model.trim="lookupToken"
              :type="showToken ? 'text' : 'password'"
              autocomplete="off"
              :placeholder="t('lookup.tokenPlaceholder')"
              @keyup.enter="lookup" /><button
              type="button"
              :aria-label="
                showToken ? t('lookup.hideToken') : t('lookup.showToken')
              "
              @click="showToken = !showToken"
            >
              <EyeOff v-if="showToken" :size="16" /><Eye
                v-else
                :size="16"
              /></button></span></label
        ><button
          class="button primary wide"
          :disabled="loading"
          @click="lookup"
        >
          <Search :size="16" />
          {{ loading ? t("lookup.searching") : t("lookup.search") }}
        </button>
        <p v-if="error" class="form-error">{{ error }}</p>
      </div>
      <div v-if="order" class="order-result">
        <div class="order-result-head">
          <div class="success-icon small"><PackageCheck /></div>
          <div>
            <span>{{
              order.status === "delivered"
                ? t("lookup.delivered")
                : t("lookup.status")
            }}</span
            ><strong>{{ order.order_no }}</strong>
          </div>
          <b>{{ order.status }}</b>
        </div>
        <div
          v-for="item in order.items.filter((entry) => entry.card_content)"
          :key="item.id"
          class="credential"
        >
          <div>
            <span>{{ item.product_name }}</span
            ><code>{{ item.card_content }}</code>
          </div>
          <button @click="copyCard(item.card_content)">
            <Copy :size="16" />
          </button>
        </div>
      </div>
      <div class="lookup-help">
        <strong>{{ t("lookup.helpTitle") }}</strong
        ><span
          >{{ t("lookup.helpBody")
          }}<template v-if="supportEmail">{{
            t("lookup.helpContact", { email: supportEmail })
          }}</template
          >。</span
        >
      </div>
    </div>
  </section>
</template>
