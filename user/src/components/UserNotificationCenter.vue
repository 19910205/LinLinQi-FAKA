<script setup lang="ts">
import { onMounted, ref } from "vue";
import { Bell, Check, RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import { fetchMyNotifications, markMyNotificationRead } from "../api";

type NotificationItem = {
  id: string;
  event_code: string;
  entity_id: string;
  title: string;
  body: string;
  read_at?: string;
  created_at: string;
};
const { t, locale } = useI18n();
const items = ref<NotificationItem[]>([]);
const loading = ref(false);
const error = ref("");
const page = ref(1);
const pageSize = ref(12);
const total = ref(0);
const unreadOnly = ref(false);
const readingIDs = ref(new Set<string>());
const date = (value: string) =>
  new Date(value).toLocaleString(locale.value, { hour12: false });
async function load(nextPage = page.value) {
  loading.value = true;
  error.value = "";
  try {
    const result = await fetchMyNotifications({
      page: nextPage,
      page_size: pageSize.value,
      unread: unreadOnly.value,
    });
    items.value = Array.isArray(result)
      ? (result as unknown as NotificationItem[])
      : result.items || [];
    total.value = Array.isArray(result)
      ? items.value.length
      : result.total || 0;
    page.value = Array.isArray(result) ? 1 : result.page || nextPage;
  } catch (reason: any) {
    error.value =
      reason?.response?.data?.message || t("notifications.loadFailed");
  } finally {
    loading.value = false;
  }
}
async function read(item: NotificationItem) {
  if (item.read_at || readingIDs.value.has(item.id)) return;
  readingIDs.value = new Set(readingIDs.value).add(item.id);
  try {
    await markMyNotificationRead(item.id);
    if (unreadOnly.value) {
      const currentPage = page.value;
      await load(currentPage);
      if (!items.value.length && currentPage > 1) await load(currentPage - 1);
    } else item.read_at = new Date().toISOString();
  } catch (reason: any) {
    error.value =
      reason?.response?.data?.message || t("notifications.readFailed");
  } finally {
    const next = new Set(readingIDs.value);
    next.delete(item.id);
    readingIDs.value = next;
  }
}
function toggleUnread() {
  page.value = 1;
  void load(1);
}
onMounted(() => void load(1));
</script>

<template>
  <section class="notification-center account-panel">
    <div class="panel-heading notification-toolbar">
      <div>
        <h2><Bell />{{ t("notifications.title") }}</h2>
        <p>{{ t("notifications.subtitle") }}</p>
      </div>
      <div class="notification-actions">
        <label class="notification-filter"
          ><input
            v-model="unreadOnly"
            type="checkbox"
            @change="toggleUnread"
          />{{ t("notifications.unreadOnly") }}</label
        >
        <button
          class="button"
          type="button"
          :disabled="loading"
          @click="load(1)"
        >
          <RefreshCw :class="{ spin: loading }" />{{
            t("notifications.refresh")
          }}
        </button>
      </div>
    </div>
    <p v-if="error" class="form-error">{{ error }}</p>
    <p v-if="loading && !items.length" class="empty-state">
      {{ t("notifications.loading") }}
    </p>
    <div v-else-if="items.length" class="notification-list">
      <article
        v-for="item in items"
        :key="item.id"
        :class="['notification-item', { unread: !item.read_at }]"
        :role="item.read_at ? undefined : 'button'"
        :tabindex="item.read_at ? undefined : 0"
        :aria-busy="readingIDs.has(item.id)"
        @click="read(item)"
        @keydown.enter.prevent="read(item)"
        @keydown.space.prevent="read(item)"
      >
        <div class="notification-icon"><Bell /></div>
        <div class="notification-copy">
          <div class="notification-title">
            <strong>{{ item.title }}</strong
            ><time>{{ date(item.created_at) }}</time>
          </div>
          <p>{{ item.body }}</p>
          <small
            >{{ item.event_code
            }}<span v-if="item.entity_id"> · {{ item.entity_id }}</span></small
          >
        </div>
        <Check
          v-if="item.read_at"
          class="notification-read"
          :title="t('notifications.read')"
        />
      </article>
    </div>
    <p v-else class="empty-state">{{ t("notifications.empty") }}</p>
    <div v-if="total > pageSize" class="notification-pagination">
      <button
        class="button"
        :disabled="page <= 1 || loading"
        @click="load(page - 1)"
      >
        {{ t("notifications.previous") }}</button
      ><span>{{ page }} / {{ Math.max(1, Math.ceil(total / pageSize)) }}</span
      ><button
        class="button"
        :disabled="page >= Math.ceil(total / pageSize) || loading"
        @click="load(page + 1)"
      >
        {{ t("notifications.next") }}
      </button>
    </div>
  </section>
</template>
