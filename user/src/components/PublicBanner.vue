<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ArrowUpRight, ImageOff } from "@lucide/vue";
import type { PublicBanner } from "../types";
import { safePublicHTTPURL } from "../utils/publicUrl";

const props = withDefaults(
  defineProps<{
    banner: PublicBanner;
    variant?: "hero" | "secondary" | "content";
  }>(),
  { variant: "secondary" },
);

const imageFailed = ref(false);

const imageURL = computed(() => safePublicHTTPURL(props.banner.image_url));
const targetURL = computed(() => safePublicHTTPURL(props.banner.target_url));
const externalTarget = computed(() => /^https?:\/\//i.test(targetURL.value));
const element = computed(() => (targetURL.value ? "a" : "article"));

watch(
  () => props.banner.image_url,
  () => {
    imageFailed.value = false;
  },
);
</script>

<template>
  <component
    :is="element"
    :class="['public-banner', `public-banner-${variant}`]"
    :href="targetURL || undefined"
    :target="externalTarget ? '_blank' : undefined"
    :rel="externalTarget ? 'noopener noreferrer' : undefined"
    :aria-label="targetURL ? banner.title : undefined"
  >
    <img
      v-if="imageURL && !imageFailed"
      :src="imageURL"
      :alt="banner.title"
      loading="lazy"
      decoding="async"
      referrerpolicy="no-referrer"
      @error="imageFailed = true"
    />
    <span v-else class="public-banner-fallback" aria-hidden="true">
      <ImageOff :size="24" />
    </span>
    <span class="public-banner-shade" aria-hidden="true"></span>
    <strong>{{ banner.title }}</strong>
    <ArrowUpRight v-if="targetURL" class="public-banner-link-icon" />
  </component>
</template>

<style scoped>
.public-banner {
  position: relative;
  display: block;
  overflow: hidden;
  min-width: 0;
  border: 1px solid var(--line);
  border-radius: 11px;
  background: var(--soft);
  color: #fff;
  isolation: isolate;
}

.public-banner[href] {
  transition:
    transform 0.2s ease,
    border-color 0.2s ease,
    box-shadow 0.2s ease;
}

.public-banner[href]:hover {
  transform: translateY(-2px);
  border-color: color-mix(in srgb, var(--text) 25%, var(--line));
  box-shadow: var(--shadow);
}

.public-banner-hero {
  min-height: clamp(230px, 30vw, 380px);
}

.public-banner-secondary {
  min-height: clamp(180px, 22vw, 260px);
}

.public-banner-content {
  min-height: clamp(170px, 22vw, 240px);
}

.public-banner img,
.public-banner-fallback,
.public-banner-shade {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.public-banner img {
  object-fit: cover;
}

.public-banner-fallback {
  display: grid;
  place-items: center;
  background:
    linear-gradient(
      135deg,
      transparent 0 46%,
      var(--line) 46% 47%,
      transparent 47%
    ),
    var(--soft);
  color: var(--muted);
}

.public-banner-shade {
  background: linear-gradient(to top, rgba(0, 0, 0, 0.76), transparent 68%);
  z-index: 1;
}

.public-banner strong {
  position: absolute;
  z-index: 2;
  left: clamp(18px, 3vw, 34px);
  right: 58px;
  bottom: clamp(18px, 3vw, 30px);
  font-size: clamp(17px, 2.4vw, 30px);
  line-height: 1.2;
  letter-spacing: -0.025em;
  text-shadow: 0 1px 16px rgba(0, 0, 0, 0.42);
}

.public-banner-secondary strong,
.public-banner-content strong {
  font-size: clamp(15px, 1.8vw, 22px);
}

.public-banner-link-icon {
  position: absolute;
  z-index: 2;
  right: clamp(18px, 3vw, 30px);
  bottom: clamp(18px, 3vw, 30px);
  width: 20px;
  height: 20px;
}

@media (max-width: 620px) {
  .public-banner-hero,
  .public-banner-secondary,
  .public-banner-content {
    min-height: 190px;
  }
}
</style>
