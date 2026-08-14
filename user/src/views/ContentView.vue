<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import {
  ArrowRight,
  Bell,
  CalendarDays,
  ChevronRight,
  RefreshCw,
  ShieldCheck,
} from "@lucide/vue";
import { fetchPost, fetchPublicContent } from "../api";
import PublicBanner from "../components/PublicBanner.vue";
import type {
  PublicAnnouncement,
  PublicBanner as Banner,
  PublicPost,
} from "../types";

const { t, locale } = useI18n();
const route = useRoute();
const kind = computed(() => String(route.meta.kind || "blog"));
const posts = ref<PublicPost[]>([]);
const announcements = ref<PublicAnnouncement[]>([]);
const post = ref<PublicPost | null>(null);
const banners = ref<Banner[]>([]);
const loading = ref(false);
const bannersLoading = ref(false);
const error = ref("");
const bannerError = ref("");
let loadSequence = 0;

const contentHeaderBanners = computed(() =>
  banners.value.filter((banner) => banner.placement === "content_header"),
);
const legal = computed(() =>
  route.params.type === "privacy"
    ? {
        title: t("content.legal.privacyTitle"),
        summary: t("content.legal.privacySummary"),
      }
    : {
        title: t("content.legal.termsTitle"),
        summary: t("content.legal.termsSummary"),
      },
);

function requestMessage(reason: unknown, fallback: string) {
  if (!reason || typeof reason !== "object") return fallback;
  const response = (reason as { response?: { data?: { message?: unknown } } })
    .response;
  return typeof response?.data?.message === "string"
    ? response.data.message
    : fallback;
}

const date = (value?: string | null) =>
  value ? new Date(value).toLocaleDateString(locale.value) : "—";

async function loadPublicBanners(requestSequence = loadSequence) {
  bannersLoading.value = true;
  bannerError.value = "";
  try {
    const content = await fetchPublicContent();
    if (requestSequence !== loadSequence) return;
    banners.value = content.banners;
    return content;
  } catch (reason: unknown) {
    if (requestSequence !== loadSequence) return;
    banners.value = [];
    bannerError.value = requestMessage(reason, t("content.bannerLoadFailed"));
    return null;
  } finally {
    if (requestSequence === loadSequence) bannersLoading.value = false;
  }
}

async function load() {
  const requestSequence = ++loadSequence;
  error.value = "";
  bannerError.value = "";
  post.value = null;
  posts.value = [];
  announcements.value = [];
  banners.value = [];

  if (kind.value === "legal") {
    loading.value = false;
    bannersLoading.value = false;
    return;
  }

  loading.value = true;
  if (kind.value === "article") {
    const [postResult] = await Promise.allSettled([
      fetchPost(String(route.params.slug || "")),
      loadPublicBanners(requestSequence),
    ]);
    if (requestSequence !== loadSequence) return;
    if (postResult.status === "fulfilled") {
      post.value = postResult.value;
    } else {
      error.value = requestMessage(postResult.reason, t("content.errLoad"));
    }
    loading.value = false;
    return;
  }

  bannersLoading.value = true;
  try {
    const content = await fetchPublicContent();
    if (requestSequence !== loadSequence) return;
    banners.value = content.banners;
    posts.value = content.posts;
    announcements.value = content.announcements;
  } catch (reason: unknown) {
    if (requestSequence !== loadSequence) return;
    error.value = requestMessage(reason, t("content.errLoad"));
  } finally {
    if (requestSequence === loadSequence) {
      loading.value = false;
      bannersLoading.value = false;
    }
  }
}

watch(() => route.fullPath, load, { immediate: true });
</script>

<template>
  <section class="section content-page">
    <div class="container">
      <div
        v-if="contentHeaderBanners.length"
        class="content-banner-grid"
        :aria-label="t('content.bannerRegionLabel')"
      >
        <PublicBanner
          v-for="banner in contentHeaderBanners"
          :key="banner.id"
          :banner="banner"
          variant="content"
        />
      </div>
      <div
        v-else-if="bannersLoading"
        class="content-banner-status"
        role="status"
      >
        {{ t("content.bannerLoading") }}
      </div>
      <div
        v-else-if="bannerError && !error"
        class="content-banner-status content-banner-error"
      >
        <span>{{ bannerError }}</span>
        <button type="button" @click="loadPublicBanners()">
          <RefreshCw :size="14" /> {{ t("content.retry") }}
        </button>
      </div>

      <p v-if="loading" class="content-loading" role="status">
        {{ t("content.loading") }}
      </p>
      <div v-else-if="error" class="content-error-state">
        <strong>{{ t("content.loadFailedTitle") }}</strong>
        <p class="form-error">{{ error }}</p>
        <button class="button secondary" type="button" @click="load">
          <RefreshCw :size="15" /> {{ t("content.retry") }}
        </button>
      </div>

      <template v-else-if="kind === 'blog'">
        <div class="content-hero">
          <span class="kicker">{{ t("kicker.insightsGuides") }}</span>
          <h1>{{ t("content.blogTitle") }}</h1>
          <p>{{ t("content.blogSubtitle") }}</p>
        </div>
        <div v-if="posts[0]" class="featured-post">
          <div>
            <span>{{ t("kicker.featured") }}</span
            ><strong>LQ</strong>
          </div>
          <article>
            <span>{{ date(posts[0].published_at) }}</span>
            <h2>{{ posts[0].title }}</h2>
            <p>{{ posts[0].summary }}</p>
            <RouterLink :to="`/blog/${posts[0].slug}`"
              >{{ t("content.readMore") }} <ArrowRight
            /></RouterLink>
          </article>
        </div>
        <div class="post-grid">
          <RouterLink
            v-for="item in posts.slice(1)"
            :key="item.id"
            :to="`/blog/${item.slug}`"
            ><span>{{ date(item.published_at) }}</span>
            <h3>{{ item.title }}</h3>
            <p>{{ item.summary }}</p>
            <i><ChevronRight /></i
          ></RouterLink>
        </div>
        <div v-if="!posts.length" class="empty">
          {{ t("content.noPosts") }}
        </div>
      </template>

      <template v-else-if="kind === 'article' && post">
        <article class="article">
          <RouterLink to="/blog">{{ t("content.backToBlog") }}</RouterLink
          ><span class="kicker">{{ t("kicker.linlinqiContent") }}</span>
          <h1>{{ post.title }}</h1>
          <div class="article-meta">
            <CalendarDays />{{ date(post.published_at) }}
          </div>
          <p class="lead">{{ post.summary }}</p>
          <div class="article-body">{{ post.content }}</div>
        </article>
      </template>

      <template v-else-if="kind === 'notice'">
        <div class="content-hero">
          <Bell /><span class="kicker">{{ t("kicker.announcements") }}</span>
          <h1>{{ t("content.noticeTitle") }}</h1>
          <p>{{ t("content.noticeSubtitle") }}</p>
        </div>
        <div class="notice-list">
          <article v-for="item in announcements" :key="item.id">
            <span>{{ date(item.created_at) }}</span>
            <div>
              <b>{{ item.title }}</b>
              <p>{{ item.content }}</p>
            </div>
            <em v-if="item.level === 'important'">{{
              t("content.important")
            }}</em>
          </article>
          <div v-if="!announcements.length" class="empty">
            {{ t("content.noNotices") }}
          </div>
        </div>
      </template>

      <template v-else-if="kind === 'legal'">
        <article class="article legal">
          <ShieldCheck /><span class="kicker">{{ t("kicker.legal") }}</span>
          <h1>{{ legal.title }}</h1>
          <p class="lead">{{ legal.summary }}</p>
          <h2>{{ t("content.legal.scopeTitle") }}</h2>
          <p>{{ t("content.legal.scopeBody") }}</p>
          <h2>{{ t("content.legal.tradeTitle") }}</h2>
          <p>{{ t("content.legal.tradeBody") }}</p>
          <h2>{{ t("content.legal.securityTitle") }}</h2>
          <p>{{ t("content.legal.securityBody") }}</p>
        </article>
      </template>
    </div>
  </section>
</template>

<style scoped>
.content-banner-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 42px;
}

.content-banner-grid > :only-child {
  grid-column: 1 / -1;
}

.content-banner-status {
  min-height: 90px;
  margin-bottom: 34px;
  border: 1px dashed var(--line);
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--muted);
  font-size: 12px;
}

.content-banner-error button {
  border: 0;
  background: transparent;
  color: var(--text);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.content-loading {
  min-height: 260px;
  display: grid;
  place-items: center;
  color: var(--muted);
}

.content-error-state {
  min-height: 320px;
  border: 1px dashed var(--line);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 12px;
  text-align: center;
}

.content-error-state .form-error {
  margin: 0;
}

.article-body {
  white-space: pre-wrap;
  font-size: 14px;
  line-height: 1.9;
  color: var(--muted);
}

@media (max-width: 620px) {
  .content-banner-grid {
    grid-template-columns: 1fr;
    margin-bottom: 28px;
  }

  .content-banner-grid > :only-child {
    grid-column: auto;
  }

  .content-banner-status {
    align-items: stretch;
    flex-direction: column;
    text-align: center;
  }
}
</style>
