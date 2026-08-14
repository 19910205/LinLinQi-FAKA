<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  availableCurrencies,
  selectCurrency,
  selectedCurrency,
  storeCurrency,
} from "../utils/money";

const { t } = useI18n();
const value = computed({
  get: () => selectedCurrency.value || storeCurrency.value,
  set: (currency: string) => selectCurrency(currency),
});
</script>

<template>
  <label class="currency-switcher">
    <span class="sr-only">{{ t("money.selectCurrency") }}</span>
    <select v-model="value" :aria-label="t('money.selectCurrency')">
      <option
        v-for="currency in availableCurrencies"
        :key="currency.code"
        :value="currency.code"
      >
        {{ currency.code }} · {{ currency.name }}
      </option>
    </select>
  </label>
</template>
