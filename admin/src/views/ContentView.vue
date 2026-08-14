<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";
import {
  BookOpenText,
  CheckCircle2,
  Clipboard,
  FileImage,
  FolderTree,
  Image,
  Library,
  LoaderCircle,
  Megaphone,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Send,
  Trash2,
  X,
} from "@lucide/vue";
import { adminApi, useAuthStore } from "../stores/auth";
import { useI18n } from "vue-i18n";
import { safeAdminHTTPURL } from "../utils/publicUrl";

type ContentTab =
  "posts" | "categories" | "announcements" | "banners" | "media";

interface PagePayload<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

interface ContentCategory {
  id: string;
  name: string;
  slug: string;
  sort: number;
  post_count: number;
  created_at: string;
  updated_at: string;
}

interface ContentPost {
  id: string;
  category_id?: string | null;
  category_name: string;
  title: string;
  slug: string;
  summary: string;
  content: string;
  cover_url: string;
  status: "draft" | "published";
  author_name: string;
  published_at?: string | null;
  seo?: { title?: string; description?: string };
  created_at: string;
  updated_at: string;
}

interface ContentAnnouncement {
  id: string;
  title: string;
  content: string;
  level: "info" | "important" | "warning";
  enabled: boolean;
  sort: number;
  created_at: string;
  updated_at: string;
}

interface ContentBanner {
  id: string;
  title: string;
  image_url: string;
  target_url: string;
  placement: "home_hero" | "home_secondary" | "content_header";
  sort: number;
  enabled: boolean;
  starts_at?: string | null;
  ends_at?: string | null;
  created_at: string;
  updated_at: string;
}

interface ContentMedia {
  id: string;
  public_url: string;
  alt_text: string;
  file_name: string;
  mime: string;
  size: number;
  sha256: string;
  uploader_name: string;
  created_at: string;
  updated_at: string;
}

interface ListState {
  page: number;
  pageSize: number;
  total: number;
  search: string;
  query: string;
  loading: boolean;
  error: string;
}

const route = useRoute();
const { t } = useI18n();
const authStore = useAuthStore();
const canManage = computed(() => authStore.hasPermission("marketing.manage"));
const tabs = computed<
  Array<{
    id: ContentTab;
    label: string;
    description: string;
    icon: typeof BookOpenText;
  }>
>(() => [
  {
    id: "posts",
    label: t("content.tabPosts"),
    description: t("content.tabPostsDesc"),
    icon: BookOpenText,
  },
  {
    id: "categories",
    label: t("content.tabCategories"),
    description: t("content.tabCategoriesDesc"),
    icon: FolderTree,
  },
  {
    id: "announcements",
    label: t("content.tabAnnouncements"),
    description: t("content.tabAnnouncementsDesc"),
    icon: Megaphone,
  },
  {
    id: "banners",
    label: t("content.tabBanners"),
    description: t("content.tabBannersDesc"),
    icon: Image,
  },
  {
    id: "media",
    label: t("content.tabMedia"),
    description: t("content.tabMediaDesc"),
    icon: Library,
  },
]);
const validTabs = new Set<ContentTab>([
  "posts",
  "categories",
  "announcements",
  "banners",
  "media",
]);
const activeTab = ref<ContentTab>("posts");
const notice = ref("");

const states = reactive<Record<ContentTab, ListState>>({
  posts: newListState(),
  categories: newListState(),
  announcements: newListState(),
  banners: newListState(),
  media: newListState(),
});
const posts = ref<ContentPost[]>([]);
const categories = ref<ContentCategory[]>([]);
const announcements = ref<ContentAnnouncement[]>([]);
const banners = ref<ContentBanner[]>([]);
const media = ref<ContentMedia[]>([]);
const categoryOptions = ref<ContentCategory[]>([]);

const postStatusFilter = ref("");
const postCategoryFilter = ref("");
const announcementLevelFilter = ref("");
const announcementEnabledFilter = ref("");
const bannerPlacementFilter = ref("");
const bannerEnabledFilter = ref("");
const mediaKindFilter = ref("");
let listRequest = 0;

const editorOpen = ref(false);
const editingID = ref("");
const editorTab = ref<ContentTab>("posts");
const saving = ref(false);
const formError = ref("");

const categoryForm = reactive({ name: "", slug: "", sort: 0, reason: "" });
const postForm = reactive({
  categoryID: "",
  title: "",
  slug: "",
  summary: "",
  content: "",
  coverURL: "",
  status: "draft" as "draft" | "published",
  publishedAt: "",
  seoTitle: "",
  seoDescription: "",
  reason: "",
});
const announcementForm = reactive({
  title: "",
  content: "",
  level: "info" as "info" | "important" | "warning",
  enabled: true,
  sort: 0,
  reason: "",
});
const bannerForm = reactive({
  title: "",
  imageURL: "",
  targetURL: "",
  placement: "home_hero" as "home_hero" | "home_secondary" | "content_header",
  sort: 0,
  enabled: true,
  startsAt: "",
  endsAt: "",
  reason: "",
});
const mediaForm = reactive({
  publicURL: "",
  altText: "",
  fileName: "",
  mime: "image/webp",
  size: 0,
  sha256: "",
  reason: "",
});
const mediaUploading = ref(false);
const bannerUploading = ref(false);
const coverUploading = ref(false);

const deleteTarget = ref<{
  tab: ContentTab;
  id: string;
  title: string;
} | null>(null);
const deleteReason = ref("");
const deleteError = ref("");
const deleting = ref(false);

const currentState = computed(() => states[activeTab.value]);
const pageCount = computed(() =>
  Math.max(
    1,
    Math.ceil(currentState.value.total / currentState.value.pageSize),
  ),
);
const pageNumbers = computed(() =>
  pageWindow(currentState.value.page, pageCount.value),
);
const currentTab = computed(
  () => tabs.value.find((item) => item.id === activeTab.value) || tabs.value[0],
);
const editorTitle = computed(() => {
  const label =
    tabs.value.find((item) => item.id === editorTab.value)?.label ||
    t("content.editorTitle");
  return `${editingID.value ? t("content.edit") : t("content.create")}${label}`;
});

const statusLabels: Record<string, string> = {
  draft: "content.statusDraft",
  published: "content.statusPublished",
};
const levelLabels: Record<string, string> = {
  info: "content.levelInfo",
  important: "content.levelImportant",
  warning: "content.levelWarning",
};
const placementLabels: Record<string, string> = {
  home_hero: "content.placementHomeHero",
  home_secondary: "content.placementHomeSecondary",
  content_header: "content.placementContentHeader",
};

function statusLabel(value: string) {
  const key = statusLabels[value];
  return key ? t(key) : value;
}

function levelLabel(value: string) {
  const key = levelLabels[value];
  return key ? t(key) : value;
}

function placementLabel(value: string) {
  const key = placementLabels[value];
  return key ? t(key) : value;
}

function newListState(): ListState {
  return {
    page: 1,
    pageSize: 20,
    total: 0,
    search: "",
    query: "",
    loading: false,
    error: "",
  };
}

function pageWindow(current: number, total: number) {
  const start = Math.max(1, Math.min(current - 2, total - 4));
  const end = Math.min(total, start + 4);
  return Array.from({ length: end - start + 1 }, (_, index) => start + index);
}

function apiMessage(error: unknown, fallback: string) {
  const failure = error as { response?: { data?: { message?: string } } };
  return failure.response?.data?.message || fallback;
}

function validReason(value: string) {
  const length = [...value.trim()].length;
  return length >= 4 && length <= 500;
}

function reasonHeaders(value: string) {
  return { "X-Change-Reason": value.trim() };
}

function formatTime(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function toLocalInput(value?: string | null) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const shifted = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return shifted.toISOString().slice(0, 16);
}

function toAPIDate(value: string) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

function formatBytes(value: number) {
  const size = Number(value || 0);
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  if (size < 1024 * 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`;
  return `${(size / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function truncate(value: string, maximum = 70) {
  const text = value.trim();
  return text.length > maximum ? `${text.slice(0, maximum)}…` : text || "—";
}

function postState(item: ContentPost) {
  if (
    item.status === "published" &&
    item.published_at &&
    new Date(item.published_at).getTime() > Date.now()
  ) {
    return { label: t("content.scheduledPublish"), className: "important" };
  }
  return {
    label: statusLabel(item.status),
    className: item.status,
  };
}

function buildParams(tab: ContentTab) {
  const state = states[tab];
  const params: Record<string, string | number> = {
    page: state.page,
    page_size: state.pageSize,
  };
  if (state.query) params.q = state.query;
  if (tab === "posts") {
    if (postStatusFilter.value) params.status = postStatusFilter.value;
    if (postCategoryFilter.value) params.category_id = postCategoryFilter.value;
  }
  if (tab === "announcements") {
    if (announcementLevelFilter.value)
      params.level = announcementLevelFilter.value;
    if (announcementEnabledFilter.value)
      params.enabled = announcementEnabledFilter.value;
  }
  if (tab === "banners") {
    if (bannerPlacementFilter.value)
      params.placement = bannerPlacementFilter.value;
    if (bannerEnabledFilter.value) params.enabled = bannerEnabledFilter.value;
  }
  if (tab === "media" && mediaKindFilter.value)
    params.kind = mediaKindFilter.value;
  return params;
}

async function loadCategoriesForSelection() {
  try {
    const { data } = await adminApi.get("/content/categories", {
      params: { page: 1, page_size: 100 },
    });
    const payload = data.data as PagePayload<ContentCategory>;
    categoryOptions.value = Array.isArray(payload?.items) ? payload.items : [];
  } catch {
    categoryOptions.value = [];
  }
}

async function loadTab(tab = activeTab.value) {
  const request = ++listRequest;
  const state = states[tab];
  state.loading = true;
  state.error = "";
  const endpoints: Record<ContentTab, string> = {
    posts: "/content/posts",
    categories: "/content/categories",
    announcements: "/content/announcements",
    banners: "/content/banners",
    media: "/content/media",
  };
  try {
    const { data } = await adminApi.get(endpoints[tab], {
      params: buildParams(tab),
    });
    if (request !== listRequest || tab !== activeTab.value) return;
    const payload = data.data as PagePayload<never>;
    const records = Array.isArray(payload?.items) ? payload.items : [];
    if (tab === "posts") posts.value = records as ContentPost[];
    if (tab === "categories") categories.value = records as ContentCategory[];
    if (tab === "announcements")
      announcements.value = records as ContentAnnouncement[];
    if (tab === "banners") banners.value = records as ContentBanner[];
    if (tab === "media") media.value = records as ContentMedia[];
    state.total = Number(payload?.total || 0);
    state.page = Number(payload?.page || state.page);
    state.pageSize = Number(payload?.page_size || state.pageSize);
    const totalPages = Math.max(1, Math.ceil(state.total / state.pageSize));
    if (state.page > totalPages && state.page > 1) {
      state.page = totalPages;
      await loadTab(tab);
    }
  } catch (error: unknown) {
    if (request !== listRequest || tab !== activeTab.value) return;
    state.total = 0;
    state.error = apiMessage(
      error,
      t("content.errLoadList", { label: currentTab.value.label }),
    );
  } finally {
    if (request === listRequest) state.loading = false;
  }
}

async function search() {
  const state = currentState.value;
  state.query = state.search.trim();
  state.page = 1;
  await loadTab();
}

async function resetFilters() {
  const state = currentState.value;
  state.search = "";
  state.query = "";
  if (activeTab.value === "posts") {
    postStatusFilter.value = "";
    postCategoryFilter.value = "";
  }
  if (activeTab.value === "announcements") {
    announcementLevelFilter.value = "";
    announcementEnabledFilter.value = "";
  }
  if (activeTab.value === "banners") {
    bannerPlacementFilter.value = "";
    bannerEnabledFilter.value = "";
  }
  if (activeTab.value === "media") mediaKindFilter.value = "";
  state.page = 1;
  await loadTab();
}

async function changePage(target: number) {
  const state = currentState.value;
  if (target < 1 || target > pageCount.value || target === state.page) return;
  state.page = target;
  await loadTab();
}

function clearForms() {
  Object.assign(categoryForm, { name: "", slug: "", sort: 0, reason: "" });
  Object.assign(postForm, {
    categoryID: "",
    title: "",
    slug: "",
    summary: "",
    content: "",
    coverURL: "",
    status: "draft",
    publishedAt: "",
    seoTitle: "",
    seoDescription: "",
    reason: "",
  });
  Object.assign(announcementForm, {
    title: "",
    content: "",
    level: "info",
    enabled: true,
    sort: 0,
    reason: "",
  });
  Object.assign(bannerForm, {
    title: "",
    imageURL: "",
    targetURL: "",
    placement: "home_hero",
    sort: 0,
    enabled: true,
    startsAt: "",
    endsAt: "",
    reason: "",
  });
  Object.assign(mediaForm, {
    publicURL: "",
    altText: "",
    fileName: "",
    mime: "image/webp",
    size: 0,
    sha256: "",
    reason: "",
  });
}

function openCreate() {
  if (!canManage.value) return;
  clearForms();
  editorTab.value = activeTab.value;
  editingID.value = "";
  formError.value = "";
  editorOpen.value = true;
}

function openCategory(item: ContentCategory) {
  if (!canManage.value) return;
  clearForms();
  editorTab.value = "categories";
  editingID.value = item.id;
  Object.assign(categoryForm, {
    name: item.name,
    slug: item.slug,
    sort: item.sort,
  });
  editorOpen.value = true;
}

function openPost(item: ContentPost, targetStatus?: "draft" | "published") {
  if (!canManage.value) return;
  clearForms();
  editorTab.value = "posts";
  editingID.value = item.id;
  Object.assign(postForm, {
    categoryID: item.category_id || "",
    title: item.title,
    slug: item.slug,
    summary: item.summary,
    content: item.content,
    coverURL: item.cover_url,
    status: targetStatus || item.status,
    publishedAt:
      targetStatus === "draft" ? "" : toLocalInput(item.published_at),
    seoTitle: item.seo?.title || "",
    seoDescription: item.seo?.description || "",
  });
  editorOpen.value = true;
}

function openAnnouncement(item: ContentAnnouncement, enabled = item.enabled) {
  if (!canManage.value) return;
  clearForms();
  editorTab.value = "announcements";
  editingID.value = item.id;
  Object.assign(announcementForm, { ...item, enabled });
  editorOpen.value = true;
}

function openBanner(item: ContentBanner, enabled = item.enabled) {
  if (!canManage.value) return;
  clearForms();
  editorTab.value = "banners";
  editingID.value = item.id;
  Object.assign(bannerForm, {
    title: item.title,
    imageURL: item.image_url,
    targetURL: item.target_url,
    placement: item.placement,
    sort: item.sort,
    enabled,
    startsAt: toLocalInput(item.starts_at),
    endsAt: toLocalInput(item.ends_at),
  });
  editorOpen.value = true;
}

function openMedia(item: ContentMedia) {
  if (!canManage.value) return;
  clearForms();
  editorTab.value = "media";
  editingID.value = item.id;
  Object.assign(mediaForm, {
    publicURL: item.public_url,
    altText: item.alt_text,
    fileName: item.file_name,
    mime: item.mime,
    size: item.size,
    sha256: item.sha256,
  });
  editorOpen.value = true;
}

function closeEditor() {
  if (saving.value) return;
  editorOpen.value = false;
  editingID.value = "";
  formError.value = "";
  clearForms();
}

function editorReason() {
  if (editorTab.value === "categories") return categoryForm.reason;
  if (editorTab.value === "posts") return postForm.reason;
  if (editorTab.value === "announcements") return announcementForm.reason;
  if (editorTab.value === "banners") return bannerForm.reason;
  return mediaForm.reason;
}

function editorPayload(): Record<string, unknown> {
  if (editorTab.value === "categories") {
    return {
      name: categoryForm.name.trim(),
      slug: categoryForm.slug.trim(),
      sort: Number(categoryForm.sort),
    };
  }
  if (editorTab.value === "posts") {
    return {
      category_id: postForm.categoryID || null,
      title: postForm.title.trim(),
      slug: postForm.slug.trim(),
      summary: postForm.summary.trim(),
      content: postForm.content.trim(),
      cover_url: postForm.coverURL.trim(),
      status: postForm.status,
      published_at:
        postForm.status === "published"
          ? toAPIDate(postForm.publishedAt)
          : null,
      seo: {
        title: postForm.seoTitle.trim(),
        description: postForm.seoDescription.trim(),
      },
    };
  }
  if (editorTab.value === "announcements") {
    return {
      title: announcementForm.title.trim(),
      content: announcementForm.content.trim(),
      level: announcementForm.level,
      enabled: announcementForm.enabled,
      sort: Number(announcementForm.sort),
    };
  }
  if (editorTab.value === "banners") {
    return {
      title: bannerForm.title.trim(),
      image_url: bannerForm.imageURL.trim(),
      target_url: bannerForm.targetURL.trim(),
      placement: bannerForm.placement,
      sort: Number(bannerForm.sort),
      enabled: bannerForm.enabled,
      starts_at: toAPIDate(bannerForm.startsAt),
      ends_at: toAPIDate(bannerForm.endsAt),
    };
  }
  return {
    public_url: mediaForm.publicURL.trim(),
    alt_text: mediaForm.altText.trim(),
    file_name: mediaForm.fileName.trim(),
    mime: mediaForm.mime,
    size: Number(mediaForm.size),
    sha256: mediaForm.sha256.trim(),
  };
}

async function uploadContentImage(event: Event) {
  if (!canManage.value) return;
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  if (!file.type.startsWith("image/")) {
    formError.value = t("content.uploadImageTypeInvalid");
    return;
  }
  mediaUploading.value = true;
  formError.value = "";
  try {
    const body = new FormData();
    body.append("file", file, file.name);
    body.append("alt_text", mediaForm.altText.trim() || file.name);
    const { data } = await adminApi.post("/content/media/upload", body, {
      headers: reasonHeaders(mediaForm.reason),
      timeout: 120_000,
    });
    const item = data.data as ContentMedia;
    mediaForm.publicURL = item.public_url;
    mediaForm.fileName = item.file_name;
    mediaForm.mime = item.mime;
    mediaForm.size = item.size;
    mediaForm.sha256 = item.sha256;
    mediaForm.altText = item.alt_text || mediaForm.altText || file.name;
    notice.value = t("content.uploadImageSuccess");
  } catch (error) {
    formError.value = apiMessage(error, t("content.uploadImageFailed"));
  } finally {
    mediaUploading.value = false;
  }
}

async function uploadBannerImage(event: Event) {
  if (!canManage.value) return;
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  if (!file.type.startsWith("image/")) {
    formError.value = t("content.errBannerImageType");
    return;
  }
  if (!validReason(bannerForm.reason)) {
    formError.value = t("content.errReasonBeforeUpload");
    return;
  }
  bannerUploading.value = true;
  formError.value = "";
  try {
    const body = new FormData();
    body.append("file", file, file.name);
    body.append("alt_text", bannerForm.title.trim() || file.name);
    const { data } = await adminApi.post("/content/media/upload", body, {
      headers: reasonHeaders(bannerForm.reason),
      timeout: 120_000,
    });
    const item = data.data as ContentMedia;
    bannerForm.imageURL = item.public_url;
    notice.value = t("content.bannerImageUploaded");
  } catch (error) {
    formError.value = apiMessage(error, t("content.errBannerImageUpload"));
  } finally {
    bannerUploading.value = false;
  }
}

async function uploadPostCover(event: Event) {
  if (!canManage.value) return;
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  if (!file.type.startsWith("image/")) {
    formError.value = t("content.errCoverImageType");
    return;
  }
  if (!validReason(postForm.reason)) {
    formError.value = t("content.errReasonBeforeUpload");
    return;
  }
  coverUploading.value = true;
  formError.value = "";
  try {
    const body = new FormData();
    body.append("file", file, file.name);
    body.append("alt_text", postForm.title.trim() || file.name);
    const { data } = await adminApi.post("/content/media/upload", body, {
      headers: reasonHeaders(postForm.reason),
      timeout: 120_000,
    });
    const item = data.data as ContentMedia;
    postForm.coverURL = item.public_url;
    notice.value = t("content.coverImageUploaded");
  } catch (error) {
    formError.value = apiMessage(error, t("content.errCoverImageUpload"));
  } finally {
    coverUploading.value = false;
  }
}

async function submitEditor() {
  if (!canManage.value) return;
  formError.value = "";
  const reason = editorReason();
  if (!validReason(reason)) {
    formError.value = t("content.errReasonLength");
    return;
  }
  if (
    editorTab.value === "posts" &&
    postForm.status === "published" &&
    postForm.publishedAt &&
    !toAPIDate(postForm.publishedAt)
  ) {
    formError.value = t("content.errPublishAtInvalid");
    return;
  }
  if (
    editorTab.value === "banners" &&
    bannerForm.startsAt &&
    bannerForm.endsAt &&
    new Date(bannerForm.endsAt) <= new Date(bannerForm.startsAt)
  ) {
    formError.value = t("content.errBannerEndBeforeStart");
    return;
  }
  const endpoints: Record<ContentTab, string> = {
    posts: "/content/posts",
    categories: "/content/categories",
    announcements: "/content/announcements",
    banners: "/content/banners",
    media: "/content/media",
  };
  const endpoint = `${endpoints[editorTab.value]}${editingID.value ? `/${encodeURIComponent(editingID.value)}` : ""}`;
  saving.value = true;
  try {
    if (editingID.value) {
      await adminApi.put(endpoint, editorPayload(), {
        headers: reasonHeaders(reason),
      });
    } else {
      await adminApi.post(endpoint, editorPayload(), {
        headers: reasonHeaders(reason),
      });
    }
    notice.value = t("content.savedNotice", { title: editorTitle.value });
    const changedTab = editorTab.value;
    closeEditor();
    if (changedTab === "categories") await loadCategoriesForSelection();
    await loadTab(activeTab.value);
  } catch (error: unknown) {
    formError.value = apiMessage(error, t("content.errSave"));
  } finally {
    saving.value = false;
  }
}

function requestDelete(tab: ContentTab, id: string, title: string) {
  if (!canManage.value) return;
  deleteTarget.value = { tab, id, title };
  deleteReason.value = "";
  deleteError.value = "";
}

function closeDelete() {
  if (deleting.value) return;
  deleteTarget.value = null;
  deleteReason.value = "";
  deleteError.value = "";
}

async function confirmDelete() {
  if (!canManage.value) return;
  if (!deleteTarget.value || deleting.value) return;
  if (!validReason(deleteReason.value)) {
    deleteError.value = t("content.errDeleteReason");
    return;
  }
  const endpoints: Record<ContentTab, string> = {
    posts: "/content/posts",
    categories: "/content/categories",
    announcements: "/content/announcements",
    banners: "/content/banners",
    media: "/content/media",
  };
  deleting.value = true;
  let deletedTab: ContentTab | null = null;
  try {
    deletedTab = deleteTarget.value.tab;
    await adminApi.delete(
      `${endpoints[deletedTab]}/${encodeURIComponent(deleteTarget.value.id)}`,
      { headers: reasonHeaders(deleteReason.value) },
    );
    notice.value = t("content.deletedNotice", {
      name: deleteTarget.value.title,
    });
    closeDelete();
    if (deletedTab === "categories") await loadCategoriesForSelection();
    await loadTab(activeTab.value);
  } catch (error: unknown) {
    deleteError.value = apiMessage(error, t("content.errDelete"));
  } finally {
    deleting.value = false;
  }
}

async function copyMediaURL(item: ContentMedia) {
  try {
    await navigator.clipboard.writeText(item.public_url);
    notice.value = t("content.copiedNotice", { name: item.file_name });
  } catch {
    states.media.error = t("content.errClipboard");
  }
}

watch(
  () => postForm.status,
  (status) => {
    if (status === "draft") postForm.publishedAt = "";
  },
);

watch(
  () => [route.path, route.meta.defaultTab] as const,
  async ([, defaultTab]) => {
    const requested = String(defaultTab || "posts") as ContentTab;
    activeTab.value = validTabs.has(requested) ? requested : "posts";
    notice.value = "";
    await Promise.all([loadCategoriesForSelection(), loadTab(activeTab.value)]);
  },
  { immediate: true },
);
</script>

<template>
  <main class="content-console">
    <header class="content-topbar">
      <button
        v-if="canManage"
        type="button"
        class="primary-button"
        @click="openCreate"
      >
        <Plus :size="14" /> {{ t("content.create") }}{{ currentTab.label }}
      </button>
    </header>

    <div v-if="notice" class="content-alert success-alert" role="status">
      <CheckCircle2 :size="15" />
      <span>{{ notice }}</span>
      <button
        type="button"
        :aria-label="t('content.closeNotice')"
        @click="notice = ''"
      >
        <X :size="13" />
      </button>
    </div>

    <section class="content-overview">
      <div>
        <span>{{ t("content.currentModule") }}</span>
        <strong>{{ currentTab.label }}</strong>
        <small>
          {{ currentTab.description }} · {{ t("content.auditNote") }}
        </small>
      </div>
      <div>
        <span>{{ t("content.filterResults") }}</span>
        <strong>{{ currentState.total.toLocaleString("zh-CN") }}</strong>
        <small>{{ t("content.serverPagination") }}</small>
      </div>
      <div>
        <span>{{ t("content.contentSafety") }}</span>
        <strong>{{ t("content.plainTextMarkdown") }}</strong>
        <small>{{ t("content.noHtmlPreview") }}</small>
      </div>
    </section>

    <form class="content-toolbar" @submit.prevent="search">
      <label class="search-field">
        <Search :size="14" />
        <input
          v-model="currentState.search"
          :placeholder="
            t('content.searchPlaceholder', { label: currentTab.label })
          "
        />
      </label>

      <template v-if="activeTab === 'posts'">
        <select
          v-model="postStatusFilter"
          :aria-label="t('content.postStatusAria')"
          @change="
            currentState.page = 1;
            loadTab();
          "
        >
          <option value="">{{ t("content.allStatus") }}</option>
          <option value="draft">{{ t("content.statusDraft") }}</option>
          <option value="published">{{ t("content.statusPublished") }}</option>
        </select>
        <select
          v-model="postCategoryFilter"
          :aria-label="t('content.postCategoryAria')"
          @change="
            currentState.page = 1;
            loadTab();
          "
        >
          <option value="">{{ t("content.allCategories") }}</option>
          <option
            v-for="item in categoryOptions"
            :key="item.id"
            :value="item.id"
          >
            {{ item.name }}
          </option>
        </select>
      </template>

      <template v-else-if="activeTab === 'announcements'">
        <select
          v-model="announcementLevelFilter"
          :aria-label="t('content.announcementLevelAria')"
          @change="
            currentState.page = 1;
            loadTab();
          "
        >
          <option value="">{{ t("content.allLevels") }}</option>
          <option value="info">{{ t("content.levelInfo") }}</option>
          <option value="important">{{ t("content.levelImportant") }}</option>
          <option value="warning">{{ t("content.levelWarning") }}</option>
        </select>
        <select
          v-model="announcementEnabledFilter"
          :aria-label="t('content.announcementEnabledAria')"
          @change="
            currentState.page = 1;
            loadTab();
          "
        >
          <option value="">{{ t("content.allStatus") }}</option>
          <option value="true">{{ t("content.enabled") }}</option>
          <option value="false">{{ t("content.disabled") }}</option>
        </select>
      </template>

      <template v-else-if="activeTab === 'banners'">
        <select
          v-model="bannerPlacementFilter"
          :aria-label="t('content.bannerPlacementAria')"
          @change="
            currentState.page = 1;
            loadTab();
          "
        >
          <option value="">{{ t("content.allPlacements") }}</option>
          <option value="home_hero">
            {{ t("content.placementHomeHero") }}
          </option>
          <option value="home_secondary">
            {{ t("content.placementHomeSecondary") }}
          </option>
          <option value="content_header">
            {{ t("content.placementContentHeader") }}
          </option>
        </select>
        <select
          v-model="bannerEnabledFilter"
          :aria-label="t('content.bannerEnabledAria')"
          @change="
            currentState.page = 1;
            loadTab();
          "
        >
          <option value="">{{ t("content.allStatus") }}</option>
          <option value="true">{{ t("content.enabled") }}</option>
          <option value="false">{{ t("content.disabled") }}</option>
        </select>
      </template>

      <select
        v-else-if="activeTab === 'media'"
        v-model="mediaKindFilter"
        :aria-label="t('content.mediaKindAria')"
        @change="
          currentState.page = 1;
          loadTab();
        "
      >
        <option value="">{{ t("content.allKinds") }}</option>
        <option value="image">{{ t("content.kindImage") }}</option>
        <option value="video">{{ t("content.kindVideo") }}</option>
        <option value="document">{{ t("content.kindDocument") }}</option>
      </select>

      <button type="submit" class="secondary-button">
        {{ t("content.search") }}
      </button>
      <button type="button" class="text-button" @click="resetFilters">
        {{ t("content.clear") }}
      </button>
      <button
        type="button"
        class="icon-button"
        :disabled="currentState.loading"
        :title="t('content.refresh')"
        @click="loadTab()"
      >
        <RefreshCw :size="14" :class="{ spin: currentState.loading }" />
      </button>
    </form>

    <div v-if="currentState.error" class="content-alert error-alert">
      <span>{{ currentState.error }}</span>
      <button type="button" @click="loadTab()">
        {{ t("content.retry") }}
      </button>
    </div>

    <section class="content-table-shell">
      <div v-if="currentState.loading" class="content-state">
        <LoaderCircle :size="20" class="spin" />
        {{ t("content.loadingRealData") }}
      </div>

      <table
        v-else-if="activeTab === 'posts' && posts.length"
        class="content-table posts-table"
      >
        <thead>
          <tr>
            <th>{{ t("content.tabPosts") }}</th>
            <th>{{ t("content.tabCategories") }}</th>
            <th>{{ t("content.statusPublishedAt") }}</th>
            <th>{{ t("content.authorUpdated") }}</th>
            <th>{{ t("content.actions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in posts" :key="item.id">
            <td :data-label="t('content.tabPosts')">
              <strong>{{ item.title }}</strong>
              <code>/posts/{{ item.slug }}</code>
              <small>{{ truncate(item.summary || item.content) }}</small>
            </td>
            <td :data-label="t('content.tabCategories')">
              {{ item.category_name || t("content.uncategorized") }}
            </td>
            <td :data-label="t('content.statusPublishedAt')">
              <span class="status-chip" :class="postState(item).className">
                {{ postState(item).label }}
              </span>
              <small>{{ formatTime(item.published_at) }}</small>
            </td>
            <td :data-label="t('content.authorUpdated')">
              <strong>
                {{ item.author_name || t("content.systemAdmin") }}
              </strong>
              <small>{{ formatTime(item.updated_at) }}</small>
            </td>
            <td :data-label="t('content.actions')" class="actions-cell">
              <button
                v-if="canManage && item.status === 'draft'"
                type="button"
                class="row-action"
                @click="openPost(item, 'published')"
              >
                <Send :size="12" /> {{ t("content.publish") }}
              </button>
              <button
                v-else-if="canManage"
                type="button"
                class="row-action"
                @click="openPost(item, 'draft')"
              >
                {{ t("content.toDraft") }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openPost(item)"
              >
                <Pencil :size="12" /> {{ t("content.edit") }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action danger-text"
                :disabled="item.status !== 'draft'"
                :title="
                  item.status !== 'draft'
                    ? t('content.deletePostDisabled')
                    : t('content.deletePost')
                "
                @click="requestDelete('posts', item.id, item.title)"
              >
                <Trash2 :size="12" /> {{ t("content.delete") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'categories' && categories.length"
        class="content-table"
      >
        <thead>
          <tr>
            <th>{{ t("content.tabCategories") }}</th>
            <th>{{ t("content.slugColumn") }}</th>
            <th>{{ t("content.sort") }}</th>
            <th>{{ t("content.postsCount") }}</th>
            <th>{{ t("content.actions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in categories" :key="item.id">
            <td :data-label="t('content.tabCategories')">
              <strong>{{ item.name }}</strong>
              <small>{{ formatTime(item.updated_at) }}</small>
            </td>
            <td :data-label="t('content.slugColumn')">
              <code>{{ item.slug }}</code>
            </td>
            <td :data-label="t('content.sort')">{{ item.sort }}</td>
            <td :data-label="t('content.postsCount')">{{ item.post_count }}</td>
            <td :data-label="t('content.actions')" class="actions-cell">
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openCategory(item)"
              >
                <Pencil :size="12" /> {{ t("content.edit") }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action danger-text"
                :disabled="item.post_count > 0"
                :title="
                  item.post_count > 0
                    ? t('content.deleteCategoryDisabled')
                    : t('content.deleteCategory')
                "
                @click="requestDelete('categories', item.id, item.name)"
              >
                <Trash2 :size="12" /> {{ t("content.delete") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'announcements' && announcements.length"
        class="content-table"
      >
        <thead>
          <tr>
            <th>{{ t("content.tabAnnouncements") }}</th>
            <th>{{ t("content.level") }}</th>
            <th>{{ t("content.status") }}</th>
            <th>{{ t("content.sortUpdated") }}</th>
            <th>{{ t("content.actions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in announcements" :key="item.id">
            <td :data-label="t('content.tabAnnouncements')">
              <strong>{{ item.title }}</strong>
              <small>{{ truncate(item.content, 90) }}</small>
            </td>
            <td :data-label="t('content.level')">
              <span class="status-chip" :class="item.level">
                {{ levelLabel(item.level) }}
              </span>
            </td>
            <td :data-label="t('content.status')">
              <span
                class="status-chip"
                :class="item.enabled ? 'published' : 'draft'"
              >
                {{
                  item.enabled ? t("content.enabled") : t("content.disabled")
                }}
              </span>
            </td>
            <td :data-label="t('content.sortUpdated')">
              <strong>{{ item.sort }}</strong>
              <small>{{ formatTime(item.updated_at) }}</small>
            </td>
            <td :data-label="t('content.actions')" class="actions-cell">
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openAnnouncement(item, !item.enabled)"
              >
                {{
                  item.enabled ? t("content.disabled") : t("content.enabled")
                }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openAnnouncement(item)"
              >
                <Pencil :size="12" /> {{ t("content.edit") }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action danger-text"
                :disabled="item.enabled"
                @click="requestDelete('announcements', item.id, item.title)"
              >
                <Trash2 :size="12" /> {{ t("content.delete") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'banners' && banners.length"
        class="content-table banners-table"
      >
        <thead>
          <tr>
            <th>{{ t("content.tabBanners") }}</th>
            <th>{{ t("content.placement") }}</th>
            <th>{{ t("content.statusSort") }}</th>
            <th>{{ t("content.validity") }}</th>
            <th>{{ t("content.actions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in banners" :key="item.id">
            <td :data-label="t('content.tabBanners')">
              <div class="banner-identity">
                <img :src="item.image_url" :alt="item.title" loading="lazy" />
                <div>
                  <strong>{{ item.title }}</strong>
                  <small>{{ truncate(item.image_url, 42) }}</small>
                </div>
              </div>
            </td>
            <td :data-label="t('content.placement')">
              {{ placementLabel(item.placement) }}
            </td>
            <td :data-label="t('content.statusSort')">
              <span
                class="status-chip"
                :class="item.enabled ? 'published' : 'draft'"
              >
                {{
                  item.enabled ? t("content.enabled") : t("content.disabled")
                }}
              </span>
              <small>{{ t("content.sortValue", { n: item.sort }) }}</small>
            </td>
            <td :data-label="t('content.validity')">
              <small>{{ formatTime(item.starts_at) }}</small>
              <small>
                {{ t("content.until", { time: formatTime(item.ends_at) }) }}
              </small>
            </td>
            <td :data-label="t('content.actions')" class="actions-cell">
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openBanner(item, !item.enabled)"
              >
                {{
                  item.enabled ? t("content.disabled") : t("content.enabled")
                }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openBanner(item)"
              >
                <Pencil :size="12" /> {{ t("content.edit") }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action danger-text"
                :disabled="item.enabled"
                @click="requestDelete('banners', item.id, item.title)"
              >
                <Trash2 :size="12" /> {{ t("content.delete") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <table
        v-else-if="activeTab === 'media' && media.length"
        class="content-table media-table"
      >
        <thead>
          <tr>
            <th>{{ t("content.mediaCdn") }}</th>
            <th>{{ t("content.kindSize") }}</th>
            <th>{{ t("content.uploader") }}</th>
            <th>{{ t("content.updatedAt") }}</th>
            <th>{{ t("content.actions") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in media" :key="item.id">
            <td :data-label="t('content.mediaCdn')">
              <strong>{{ item.file_name }}</strong>
              <small>{{ item.alt_text }}</small>
              <a
                :href="safeAdminHTTPURL(item.public_url) || undefined"
                target="_blank"
                rel="noopener noreferrer"
              >
                {{ truncate(item.public_url, 62) }}
              </a>
            </td>
            <td :data-label="t('content.kindSize')">
              <code>{{ item.mime }}</code>
              <small>{{ formatBytes(item.size) }}</small>
            </td>
            <td :data-label="t('content.uploader')">
              {{ item.uploader_name || t("content.systemAdmin") }}
            </td>
            <td :data-label="t('content.updatedAt')">
              {{ formatTime(item.updated_at) }}
            </td>
            <td :data-label="t('content.actions')" class="actions-cell">
              <button
                type="button"
                class="row-action"
                @click="copyMediaURL(item)"
              >
                <Clipboard :size="12" /> {{ t("content.copyUrl") }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action"
                @click="openMedia(item)"
              >
                <Pencil :size="12" /> {{ t("content.edit") }}
              </button>
              <button
                v-if="canManage"
                type="button"
                class="row-action danger-text"
                @click="requestDelete('media', item.id, item.file_name)"
              >
                <Trash2 :size="12" /> {{ t("content.delete") }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <div v-else class="content-state empty-state">
        <component :is="currentTab.icon" :size="24" />
        <strong>
          {{ t("content.emptyTitle", { label: currentTab.label }) }}
        </strong>
        <span>{{ t("content.emptyHint") }}</span>
      </div>
    </section>

    <footer class="content-pagination">
      <span>
        {{
          t("content.pageInfo", {
            page: currentState.page,
            pages: pageCount,
            total: currentState.total,
          })
        }}
      </span>
      <div>
        <button
          type="button"
          :disabled="currentState.page <= 1 || currentState.loading"
          @click="changePage(currentState.page - 1)"
        >
          {{ t("content.prevPage") }}
        </button>
        <button
          v-for="page in pageNumbers"
          :key="page"
          type="button"
          :class="{ active: page === currentState.page }"
          :disabled="currentState.loading"
          @click="changePage(page)"
        >
          {{ page }}
        </button>
        <button
          type="button"
          :disabled="currentState.page >= pageCount || currentState.loading"
          @click="changePage(currentState.page + 1)"
        >
          {{ t("content.nextPage") }}
        </button>
      </div>
    </footer>

    <div
      v-if="editorOpen && canManage"
      class="modal-backdrop"
      @click.self="closeEditor"
    >
      <section
        class="editor-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="editorTitle"
      >
        <header>
          <div>
            <span>{{ t("content.editorOps") }}</span>
            <h2>{{ editorTitle }}</h2>
          </div>
          <button type="button" :disabled="saving" @click="closeEditor">
            <X :size="17" />
          </button>
        </header>

        <form class="editor-form" @submit.prevent="submitEditor">
          <template v-if="editorTab === 'categories'">
            <div class="form-grid two-columns">
              <label>
                <span>{{ t("content.categoryName") }}</span>
                <input v-model="categoryForm.name" required maxlength="100" />
              </label>
              <label>
                <span>{{ t("content.slug") }}</span>
                <input
                  v-model="categoryForm.slug"
                  required
                  maxlength="120"
                  pattern="[A-Za-z0-9][A-Za-z0-9-]*"
                  placeholder="help-center"
                />
              </label>
              <label>
                <span>{{ t("content.sort") }}</span>
                <input
                  v-model.number="categoryForm.sort"
                  type="number"
                  min="0"
                  max="1000000"
                  required
                />
              </label>
            </div>
          </template>

          <template v-else-if="editorTab === 'posts'">
            <div class="form-grid two-columns">
              <label>
                <span>{{ t("content.postTitle") }}</span>
                <input v-model="postForm.title" required maxlength="220" />
              </label>
              <label>
                <span>{{ t("content.slug") }}</span>
                <input
                  v-model="postForm.slug"
                  required
                  maxlength="240"
                  pattern="[A-Za-z0-9][A-Za-z0-9-]*"
                  placeholder="getting-started"
                />
              </label>
              <label>
                <span>{{ t("content.tabCategories") }}</span>
                <select v-model="postForm.categoryID">
                  <option value="">{{ t("content.uncategorized") }}</option>
                  <option
                    v-for="item in categoryOptions"
                    :key="item.id"
                    :value="item.id"
                  >
                    {{ item.name }}
                  </option>
                </select>
              </label>
              <label>
                <span>{{ t("content.publishStatus") }}</span>
                <select v-model="postForm.status">
                  <option value="draft">{{ t("content.statusDraft") }}</option>
                  <option value="published">
                    {{ t("content.publishSchedule") }}
                  </option>
                </select>
              </label>
              <label v-if="postForm.status === 'published'">
                <span>{{ t("content.publishAtHint") }}</span>
                <input v-model="postForm.publishedAt" type="datetime-local" />
              </label>
              <div class="media-field">
                <label>
                  <span>{{ t("content.coverUrl") }}</span>
                  <input
                    v-model="postForm.coverURL"
                    type="text"
                    inputmode="url"
                    maxlength="1000"
                    placeholder="https://cdn.example.com/cover.webp"
                  />
                </label>
                <label
                  :class="[
                    'media-upload-button',
                    { disabled: !canManage || coverUploading },
                  ]"
                  ><LoaderCircle
                    v-if="coverUploading"
                    :size="14"
                    class="spin" /><Image v-else :size="14" />
                  {{
                    coverUploading
                      ? t("content.uploadingCoverImage")
                      : t("content.uploadCoverImage")
                  }}
                  <input
                    type="file"
                    accept="image/jpeg,image/png,image/webp,image/gif"
                    :disabled="!canManage || coverUploading"
                    @change="uploadPostCover"
                /></label>
              </div>
              <label class="full-width">
                <span>{{ t("content.summary") }}</span>
                <textarea v-model="postForm.summary" rows="3" maxlength="600" />
              </label>
            </div>
            <div class="markdown-grid">
              <label>
                <span>{{ t("content.bodyMarkdown") }}</span>
                <textarea
                  v-model="postForm.content"
                  required
                  rows="16"
                  maxlength="500000"
                  spellcheck="true"
                />
                <small>{{ t("content.noExecutableHtml") }}</small>
              </label>
              <section
                class="safe-preview"
                :aria-label="t('content.safePreview')"
              >
                <header>
                  <FileImage :size="13" /> {{ t("content.safePreview") }}
                </header>
                <pre>{{ postForm.content || t("content.previewEmpty") }}</pre>
              </section>
            </div>
            <fieldset>
              <legend>{{ t("content.seoInfo") }}</legend>
              <div class="form-grid two-columns">
                <label>
                  <span>{{ t("content.seoTitle") }}</span>
                  <input v-model="postForm.seoTitle" maxlength="120" />
                </label>
                <label>
                  <span>{{ t("content.seoDescription") }}</span>
                  <textarea
                    v-model="postForm.seoDescription"
                    rows="2"
                    maxlength="320"
                  />
                </label>
              </div>
            </fieldset>
          </template>

          <template v-else-if="editorTab === 'announcements'">
            <div class="form-grid two-columns">
              <label class="full-width">
                <span>{{ t("content.announcementTitle") }}</span>
                <input
                  v-model="announcementForm.title"
                  required
                  maxlength="160"
                />
              </label>
              <label>
                <span>{{ t("content.level") }}</span>
                <select v-model="announcementForm.level">
                  <option value="info">{{ t("content.levelInfo") }}</option>
                  <option value="important">
                    {{ t("content.levelImportant") }}
                  </option>
                  <option value="warning">
                    {{ t("content.levelWarning") }}
                  </option>
                </select>
              </label>
              <label>
                <span>{{ t("content.sort") }}</span>
                <input
                  v-model.number="announcementForm.sort"
                  type="number"
                  min="0"
                  max="1000000"
                  required
                />
              </label>
              <label class="full-width">
                <span>{{ t("content.announcementBody") }}</span>
                <textarea
                  v-model="announcementForm.content"
                  required
                  rows="9"
                  maxlength="20000"
                />
              </label>
              <label class="switch-row full-width">
                <input v-model="announcementForm.enabled" type="checkbox" />
                <span>{{ t("content.enableAnnouncement") }}</span>
              </label>
            </div>
          </template>

          <template v-else-if="editorTab === 'banners'">
            <div class="form-grid two-columns">
              <label>
                <span>{{ t("content.bannerTitle") }}</span>
                <input v-model="bannerForm.title" required maxlength="160" />
              </label>
              <label>
                <span>{{ t("content.placement") }}</span>
                <select v-model="bannerForm.placement">
                  <option value="home_hero">
                    {{ t("content.placementHomeHero") }}
                  </option>
                  <option value="home_secondary">
                    {{ t("content.placementHomeSecondary") }}
                  </option>
                  <option value="content_header">
                    {{ t("content.placementContentHeader") }}
                  </option>
                </select>
              </label>
              <label class="full-width">
                <span>{{ t("content.imageUrl") }}</span>
                <input
                  v-model="bannerForm.imageURL"
                  type="text"
                  required
                  maxlength="1000"
                  placeholder="https://cdn.example.com/banner.webp"
                />
              </label>
              <label class="full-width upload-dropzone banner-upload-zone">
                <span>{{ t("content.uploadBannerImage") }}</span>
                <input
                  type="file"
                  accept="image/jpeg,image/png,image/webp,image/gif"
                  :disabled="bannerUploading"
                  @change="uploadBannerImage"
                />
                <small>{{
                  bannerUploading
                    ? t("content.uploadingBannerImage")
                    : t("content.uploadBannerImageHint")
                }}</small>
              </label>
              <label class="full-width">
                <span>{{ t("content.targetUrl") }}</span>
                <input
                  v-model="bannerForm.targetURL"
                  type="url"
                  maxlength="1000"
                  placeholder="https://shop.example.com/promotion"
                />
              </label>
              <label>
                <span>{{ t("content.startAt") }}</span>
                <input v-model="bannerForm.startsAt" type="datetime-local" />
              </label>
              <label>
                <span>{{ t("content.endAt") }}</span>
                <input v-model="bannerForm.endsAt" type="datetime-local" />
              </label>
              <label>
                <span>{{ t("content.sort") }}</span>
                <input
                  v-model.number="bannerForm.sort"
                  type="number"
                  min="0"
                  max="1000000"
                  required
                />
              </label>
              <label class="switch-row">
                <input v-model="bannerForm.enabled" type="checkbox" />
                <span>{{ t("content.enableBanner") }}</span>
              </label>
            </div>
            <figure v-if="bannerForm.imageURL" class="banner-preview">
              <img
                :src="bannerForm.imageURL"
                :alt="bannerForm.title || t('content.bannerPreviewAlt')"
              />
              <figcaption>{{ t("content.bannerPreviewNote") }}</figcaption>
            </figure>
          </template>

          <template v-else>
            <div class="registration-note">
              <Library :size="18" />
              <div>
                <strong>{{ t("content.cdnRegistrationTitle") }}</strong>
                <p>{{ t("content.cdnRegistrationNote") }}</p>
              </div>
            </div>
            <label class="full-width upload-dropzone">
              <span>{{ t("content.uploadDirectLabel") }}</span>
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp,image/gif"
                @change="uploadContentImage"
                :disabled="mediaUploading"
              />
              <small>{{
                mediaUploading
                  ? t("content.uploading")
                  : t("content.uploadHint")
              }}</small>
            </label>
            <div class="form-grid two-columns">
              <label class="full-width">
                <span>{{ t("content.publicUrl") }}</span>
                <input
                  v-model="mediaForm.publicURL"
                  type="url"
                  required
                  maxlength="1000"
                  placeholder="https://cdn.example.com/assets/manual.pdf"
                />
              </label>
              <label>
                <span>{{ t("content.fileName") }}</span>
                <input v-model="mediaForm.fileName" required maxlength="255" />
              </label>
              <label>
                <span>{{ t("content.mimeType") }}</span>
                <select v-model="mediaForm.mime">
                  <option value="image/jpeg">
                    {{ t("content.mimeJpeg") }}
                  </option>
                  <option value="image/png">{{ t("content.mimePng") }}</option>
                  <option value="image/webp">
                    {{ t("content.mimeWebp") }}
                  </option>
                  <option value="image/gif">{{ t("content.mimeGif") }}</option>
                  <option value="video/mp4">{{ t("content.mimeMp4") }}</option>
                  <option value="video/webm">
                    {{ t("content.mimeWebm") }}
                  </option>
                  <option value="application/pdf">
                    {{ t("content.mimePdf") }}
                  </option>
                </select>
              </label>
              <label class="full-width">
                <span>{{ t("content.altText") }}</span>
                <input v-model="mediaForm.altText" required maxlength="300" />
              </label>
              <label>
                <span>{{ t("content.fileSize") }}</span>
                <input
                  v-model.number="mediaForm.size"
                  type="number"
                  min="1"
                  max="10000000000000"
                  required
                />
              </label>
              <label>
                <span>{{ t("content.sha256") }}</span>
                <input
                  v-model="mediaForm.sha256"
                  maxlength="64"
                  pattern="([A-Fa-f0-9]{64})?"
                  :placeholder="t('content.sha256Placeholder')"
                />
              </label>
            </div>
          </template>

          <label class="reason-field">
            <span>{{ t("content.reason") }}</span>
            <textarea
              v-if="editorTab === 'categories'"
              v-model="categoryForm.reason"
              required
              rows="2"
              minlength="4"
              maxlength="500"
            />
            <textarea
              v-else-if="editorTab === 'posts'"
              v-model="postForm.reason"
              required
              rows="2"
              minlength="4"
              maxlength="500"
            />
            <textarea
              v-else-if="editorTab === 'announcements'"
              v-model="announcementForm.reason"
              required
              rows="2"
              minlength="4"
              maxlength="500"
            />
            <textarea
              v-else-if="editorTab === 'banners'"
              v-model="bannerForm.reason"
              required
              rows="2"
              minlength="4"
              maxlength="500"
            />
            <textarea
              v-else
              v-model="mediaForm.reason"
              required
              rows="2"
              minlength="4"
              maxlength="500"
            />
            <small>{{ t("content.reasonHint") }}</small>
          </label>

          <p v-if="formError" class="form-error">{{ formError }}</p>
          <footer>
            <button
              type="button"
              class="secondary-button"
              :disabled="saving"
              @click="closeEditor"
            >
              {{ t("content.cancel") }}
            </button>
            <button type="submit" class="primary-button" :disabled="saving">
              <LoaderCircle v-if="saving" :size="14" class="spin" />
              {{ saving ? t("content.saving") : t("content.confirmSave") }}
            </button>
          </footer>
        </form>
      </section>
    </div>

    <div
      v-if="deleteTarget && canManage"
      class="modal-backdrop"
      @click.self="closeDelete"
    >
      <section class="delete-modal" role="alertdialog" aria-modal="true">
        <header>
          <Trash2 :size="18" />
          <div>
            <span>{{ t("content.irreversible") }}</span>
            <h2>
              {{ t("content.deleteTitle", { name: deleteTarget.title }) }}
            </h2>
          </div>
        </header>
        <p>{{ t("content.deleteNote") }}</p>
        <label>
          <span>{{ t("content.deleteReason") }}</span>
          <textarea
            v-model="deleteReason"
            rows="3"
            minlength="4"
            maxlength="500"
            required
          />
        </label>
        <p v-if="deleteError" class="form-error">{{ deleteError }}</p>
        <footer>
          <button
            type="button"
            class="secondary-button"
            :disabled="deleting"
            @click="closeDelete"
          >
            {{ t("content.cancel") }}
          </button>
          <button
            type="button"
            class="danger-button"
            :disabled="deleting"
            @click="confirmDelete"
          >
            <LoaderCircle v-if="deleting" :size="14" class="spin" />
            {{ deleting ? t("content.deleting") : t("content.confirmDelete") }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>

<style scoped>
.content-console {
  display: grid;
  gap: 14px;
  color: var(--text);
}

.content-topbar,
.content-pagination,
.content-pagination > div,
.actions-cell,
.content-alert,
.editor-modal > header,
.editor-form > footer,
.delete-modal > header,
.delete-modal > footer {
  display: flex;
  align-items: center;
}

.content-topbar,
.content-pagination,
.editor-modal > header {
  justify-content: space-between;
}

.content-tabs {
  display: flex;
  gap: 3px;
  max-width: calc(100vw - 360px);
  padding: 3px;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}

.content-tabs button {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 40px;
  padding: 0 11px;
  border: 0;
  border-radius: 7px;
  color: var(--muted);
  background: transparent;
  white-space: nowrap;
}

.content-tabs button > span {
  display: grid;
  gap: 2px;
  font-size: 10px;
  font-weight: 700;
  text-align: left;
}

.content-tabs small {
  font-size: 7px;
  font-weight: 400;
  opacity: 0.78;
}

.content-tabs button.active {
  color: var(--surface);
  background: var(--text);
}

.primary-button,
.secondary-button,
.danger-button,
.text-button,
.icon-button,
.content-pagination button,
.row-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  min-height: 35px;
  padding: 0 12px;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text);
  background: var(--surface);
  font-size: 9px;
  font-weight: 700;
}

.primary-button {
  color: var(--surface);
  border-color: var(--text);
  background: var(--text);
}

.danger-button {
  color: white;
  border-color: #b42318;
  background: #b42318;
}

.text-button,
.row-action {
  padding: 0 6px;
  border-color: transparent;
  background: transparent;
}

.icon-button {
  width: 35px;
  padding: 0;
}

button:disabled {
  opacity: 0.42;
  cursor: not-allowed;
}

.content-alert {
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid;
  border-radius: 8px;
  font-size: 9px;
}

.content-alert span {
  flex: 1;
}

.content-alert button {
  border: 0;
  color: inherit;
  background: transparent;
}

.success-alert {
  color: #166534;
  border-color: #86efac;
  background: #f0fdf4;
}

.error-alert {
  color: #991b1b;
  border-color: #fecaca;
  background: #fef2f2;
}

:global([data-theme="dark"]) .success-alert {
  color: #bbf7d0;
  border-color: #166534;
  background: #052e16;
}

:global([data-theme="dark"]) .error-alert {
  color: #fecaca;
  border-color: #7f1d1d;
  background: #450a0a;
}

.content-overview {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 9px;
}

.content-overview > div {
  min-height: 92px;
  padding: 15px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}

.content-overview span,
.content-overview small {
  display: block;
  color: var(--muted);
  font-size: 8px;
}

.content-overview strong {
  display: block;
  margin: 9px 0 5px;
  font-size: 17px;
}

.content-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}

.search-field {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 220px;
  flex: 1;
  padding: 0 10px;
  color: var(--muted);
}

.search-field input {
  width: 100%;
  height: 33px;
  border: 0;
  outline: 0;
  color: var(--text);
  background: transparent;
  font-size: 10px;
}

.content-toolbar select {
  height: 33px;
  padding: 0 28px 0 9px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--text);
  background: var(--surface);
  font-size: 9px;
}

.content-table-shell {
  min-height: 290px;
  overflow-x: auto;
  border: 1px solid var(--line);
  border-radius: 9px;
  background: var(--surface);
}

.content-table {
  width: 100%;
  min-width: 850px;
  border-collapse: collapse;
}

.content-table th {
  padding: 11px 13px;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
  background: var(--soft);
  font-size: 8px;
  font-weight: 600;
  text-align: left;
  white-space: nowrap;
}

.content-table td {
  padding: 12px 13px;
  border-bottom: 1px solid var(--line);
  font-size: 9px;
  vertical-align: middle;
}

.content-table tr:last-child td {
  border-bottom: 0;
}

.content-table td > strong,
.content-table td > code,
.content-table td > small,
.content-table td > a {
  display: block;
}

.content-table td > strong {
  font-size: 10px;
}

.content-table td > code,
.content-table td > small,
.content-table td > a {
  margin-top: 4px;
  color: var(--muted);
  font-size: 8px;
}

.content-table td > a {
  max-width: 420px;
  color: var(--text);
  text-decoration: underline;
  text-underline-offset: 2px;
}

.actions-cell {
  gap: 3px;
  white-space: nowrap;
}

.row-action {
  min-height: 28px;
  color: var(--muted);
}

.row-action:hover:not(:disabled) {
  color: var(--text);
  background: var(--soft);
}

.danger-text {
  color: var(--danger);
}

.status-chip {
  display: inline-flex;
  min-height: 22px;
  align-items: center;
  padding: 0 7px;
  border: 1px solid var(--line);
  border-radius: 999px;
  color: var(--muted);
  background: var(--surface-2);
  font-size: 8px;
  font-weight: 700;
}

.status-chip.published,
.status-chip.info {
  color: var(--success);
  border-color: color-mix(in srgb, var(--success) 35%, var(--line));
  background: color-mix(in srgb, var(--success) 8%, var(--surface));
}

.status-chip.important {
  color: var(--text);
  border-color: var(--text);
}

.status-chip.warning {
  color: var(--warn);
  border-color: color-mix(in srgb, var(--warn) 40%, var(--line));
  background: color-mix(in srgb, var(--warn) 8%, var(--surface));
}

.banner-identity {
  display: flex;
  align-items: center;
  gap: 9px;
}

.banner-identity img {
  width: 68px;
  height: 40px;
  object-fit: cover;
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--soft);
}

.banner-identity strong,
.banner-identity small {
  display: block;
}

.banner-identity small {
  max-width: 220px;
  margin-top: 4px;
  overflow: hidden;
  color: var(--muted);
  font-size: 8px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.content-state {
  min-height: 290px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  font-size: 10px;
}

.empty-state {
  flex-direction: column;
}

.empty-state strong {
  color: var(--text);
}

.empty-state span {
  font-size: 8px;
}

.content-pagination {
  gap: 12px;
  color: var(--muted);
  font-size: 8px;
}

.content-pagination > div {
  gap: 4px;
}

.content-pagination button {
  min-width: 31px;
  min-height: 30px;
  padding: 0 9px;
  color: var(--muted);
}

.content-pagination button.active {
  color: var(--surface);
  border-color: var(--text);
  background: var(--text);
}

.modal-backdrop {
  position: fixed;
  z-index: 90;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgb(0 0 0 / 55%);
  backdrop-filter: blur(3px);
}

.editor-modal,
.delete-modal {
  width: min(880px, 100%);
  max-height: calc(100vh - 48px);
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface);
  box-shadow: var(--shadow);
}

.editor-modal > header {
  position: sticky;
  z-index: 2;
  top: 0;
  gap: 12px;
  padding: 16px 18px;
  border-bottom: 1px solid var(--line);
  background: var(--surface);
}

.editor-modal > header span,
.delete-modal > header span {
  color: var(--muted);
  font-size: 7px;
  font-weight: 700;
  letter-spacing: 0.13em;
}

.editor-modal h2,
.delete-modal h2 {
  margin: 4px 0 0;
  font-size: 17px;
}

.editor-modal > header > button {
  width: 34px;
  height: 34px;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--muted);
  background: var(--surface);
}

.editor-form {
  display: grid;
  gap: 17px;
  padding: 18px;
}

.form-grid {
  display: grid;
  gap: 13px;
}

.two-columns {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.full-width {
  grid-column: 1 / -1;
}

.media-field {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 9px;
  align-items: end;
}
.media-field > label:first-child {
  min-width: 0;
}
.media-upload-button {
  display: inline-flex !important;
  min-height: 39px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 11px;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text) !important;
  background: var(--surface-2);
  white-space: nowrap;
  cursor: pointer;
  font-size: 9px;
  font-weight: 700;
}
.media-upload-button.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.media-upload-button input {
  display: none;
}

.editor-form label,
.delete-modal label {
  display: grid;
  align-content: start;
  gap: 7px;
  color: var(--muted);
  font-size: 9px;
  font-weight: 600;
}

.editor-form input,
.editor-form select,
.editor-form textarea,
.delete-modal textarea {
  width: 100%;
  min-height: 39px;
  padding: 9px 10px;
  resize: vertical;
  outline: 0;
  border: 1px solid var(--line);
  border-radius: 7px;
  color: var(--text);
  background: var(--surface-2);
  font-size: 10px;
  font-weight: 400;
  line-height: 1.65;
}

.editor-form input:focus,
.editor-form select:focus,
.editor-form textarea:focus,
.delete-modal textarea:focus {
  border-color: var(--text);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--text) 9%, transparent);
}

.editor-form label > small,
.reason-field small {
  color: var(--muted);
  font-size: 8px;
  font-weight: 400;
}

.switch-row {
  display: flex !important;
  align-items: center;
  align-self: end;
  min-height: 39px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
}

.switch-row input {
  width: 14px;
  min-height: 14px;
  accent-color: var(--text);
}

.markdown-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.05fr) minmax(0, 0.95fr);
  gap: 12px;
}

.safe-preview {
  min-height: 320px;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface-2);
}

.safe-preview header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 12px;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
  font-size: 8px;
  font-weight: 700;
}

.safe-preview pre {
  max-height: 385px;
  margin: 0;
  padding: 13px;
  overflow: auto;
  color: var(--text);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 9px;
  line-height: 1.75;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.editor-form fieldset {
  padding: 13px;
  border: 1px solid var(--line);
  border-radius: 8px;
}

.editor-form legend {
  padding: 0 6px;
  color: var(--muted);
  font-size: 8px;
  font-weight: 700;
}

.banner-preview {
  margin: 0;
  padding: 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface-2);
}

.banner-preview img {
  width: 100%;
  max-height: 220px;
  display: block;
  object-fit: contain;
  border-radius: 5px;
}

.banner-preview figcaption {
  margin-top: 8px;
  color: var(--muted);
  font-size: 8px;
}

.registration-note {
  display: flex;
  gap: 10px;
  padding: 12px;
  color: var(--text);
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--soft);
}

.registration-note svg {
  flex: 0 0 auto;
}

.registration-note strong,
.registration-note p {
  display: block;
  margin: 0;
}

.registration-note p {
  margin-top: 5px;
  color: var(--muted);
  font-size: 8px;
  line-height: 1.65;
}

.reason-field {
  padding-top: 14px;
  border-top: 1px solid var(--line);
}

.form-error {
  margin: 0;
  color: var(--danger);
  font-size: 9px;
}

.editor-form > footer,
.delete-modal > footer {
  justify-content: flex-end;
  gap: 8px;
}

.delete-modal {
  width: min(480px, 100%);
  padding: 18px;
}

.delete-modal > header {
  gap: 10px;
  color: var(--danger);
}

.delete-modal > header h2 {
  color: var(--text);
}

.delete-modal > p {
  margin: 16px 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.7;
}

.delete-modal > footer {
  margin-top: 16px;
}

.spin {
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 980px) {
  .content-topbar {
    align-items: stretch;
    flex-direction: column;
  }

  .content-tabs {
    max-width: none;
  }

  .content-toolbar {
    flex-wrap: wrap;
  }

  .search-field {
    min-width: 100%;
  }
}

@media (max-width: 720px) {
  .content-overview {
    grid-template-columns: 1fr;
  }

  .content-overview > div {
    min-height: 76px;
  }

  .content-toolbar select {
    min-width: calc(50% - 4px);
    flex: 1;
  }

  .media-field {
    grid-template-columns: 1fr;
  }

  .content-table-shell {
    overflow: visible;
    border: 0;
    background: transparent;
  }

  .content-table,
  .content-table tbody,
  .content-table tr,
  .content-table td {
    width: 100%;
    display: block;
    min-width: 0;
  }

  .content-table thead {
    display: none;
  }

  .content-table tr {
    margin-bottom: 10px;
    overflow: hidden;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--surface);
  }

  .content-table td {
    min-height: 42px;
    padding: 10px 12px 10px 38%;
    position: relative;
  }

  .content-table td::before {
    content: attr(data-label);
    width: 34%;
    position: absolute;
    left: 12px;
    top: 11px;
    color: var(--muted);
    font-size: 8px;
  }

  .actions-cell {
    justify-content: flex-end;
    white-space: normal;
  }

  .content-pagination {
    align-items: flex-start;
    flex-direction: column;
  }

  .modal-backdrop {
    align-items: end;
    padding: 0;
  }

  .editor-modal,
  .delete-modal {
    width: 100%;
    max-height: 94vh;
    border-radius: 14px 14px 0 0;
  }

  .two-columns,
  .markdown-grid {
    grid-template-columns: 1fr;
  }

  .full-width {
    grid-column: auto;
  }

  .safe-preview {
    min-height: 230px;
  }
}
</style>
