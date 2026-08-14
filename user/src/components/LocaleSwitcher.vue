<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { SUPPORTED_LOCALES, setLocale } from "../i18n";
import type { LocaleCode } from "../i18n";

const { locale } = useI18n();
const loading = ref(false);

const current = computed(() =>
  SUPPORTED_LOCALES.find((item) => item.code === locale.value),
);

async function change(event: Event) {
  const select = event.target as HTMLSelectElement;
  const code = select.value as LocaleCode;
  loading.value = true;
  try {
    await setLocale(code);
  } catch {
    select.value = locale.value;
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <select
    class="locale-switcher"
    :aria-label="current?.label"
    :aria-busy="loading"
    :disabled="loading"
    :value="locale"
    @change="change"
  >
    <option
      v-for="item in SUPPORTED_LOCALES"
      :key="item.code"
      :value="item.code"
    >
      {{ item.flag }} {{ item.label }}
    </option>
  </select>
</template>

<style scoped>
.locale-switcher {
  height: 30px;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--surface);
  color: var(--text);
  padding: 0 6px;
  font-size: 11px;
  cursor: pointer;
  outline: none;
}
.locale-switcher:hover {
  border-color: color-mix(in srgb, var(--text) 35%, var(--line));
}
</style>
