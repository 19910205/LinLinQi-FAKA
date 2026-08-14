<script setup lang="ts">
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";
import { ArrowLeft } from "@lucide/vue";

const { t } = useI18n();
</script>
<template>
  <section class="not-found">
    <span>404</span>
    <h1>{{ t("notfound.title") }}</h1>
    <p>{{ t("notfound.subtitle") }}</p>
    <RouterLink class="button primary" to="/"
      ><ArrowLeft />{{ t("notfound.backHome") }}</RouterLink
    >
  </section>
</template>
