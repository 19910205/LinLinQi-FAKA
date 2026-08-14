<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ChevronLeft, ChevronRight, Pause, Play } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import type { PublicBanner as Banner } from "../types";
import PublicBanner from "./PublicBanner.vue";

const props = withDefaults(
  defineProps<{ slides: Banner[]; interval?: number }>(),
  { interval: 5200 },
);
const { t } = useI18n();
const current = ref(0);
const paused = ref(false);
const pointerStart = ref<number | null>(null);
let timer: number | undefined;

const activeSlide = computed(() => props.slides[current.value] || null);

function stopTimer() {
  window.clearInterval(timer);
  timer = undefined;
}

function startTimer() {
  stopTimer();
  if (
    paused.value ||
    props.slides.length < 2 ||
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  )
    return;
  timer = window.setInterval(() => next(), Math.max(3000, props.interval));
}

function go(index: number) {
  if (!props.slides.length) return;
  current.value = (index + props.slides.length) % props.slides.length;
  startTimer();
}

function previous() {
  go(current.value - 1);
}

function next() {
  go(current.value + 1);
}

function togglePaused() {
  paused.value = !paused.value;
  startTimer();
}

function beginSwipe(event: PointerEvent) {
  pointerStart.value = event.clientX;
}

function endSwipe(event: PointerEvent) {
  if (pointerStart.value === null) return;
  const distance = event.clientX - pointerStart.value;
  pointerStart.value = null;
  if (Math.abs(distance) < 45) return;
  distance > 0 ? previous() : next();
}

watch(
  () => props.slides.map((slide) => slide.id).join(","),
  () => {
    current.value = Math.min(
      current.value,
      Math.max(0, props.slides.length - 1),
    );
    startTimer();
  },
);
watch(paused, startTimer);
onMounted(startTimer);
onBeforeUnmount(stopTimer);
</script>

<template>
  <section
    v-if="activeSlide"
    class="home-carousel"
    role="region"
    :aria-label="t('home.carouselAria')"
    aria-roledescription="carousel"
    tabindex="0"
    @mouseenter="paused = true"
    @mouseleave="paused = false"
    @focusin="paused = true"
    @focusout="paused = false"
    @keydown.left.prevent="previous"
    @keydown.right.prevent="next"
    @pointerdown="beginSwipe"
    @pointerup="endSwipe"
    @pointercancel="pointerStart = null"
  >
    <Transition name="carousel-fade" mode="out-in">
      <PublicBanner
        :key="activeSlide.id"
        :banner="activeSlide"
        variant="hero"
      />
    </Transition>
    <button
      v-if="slides.length > 1"
      type="button"
      class="carousel-arrow previous"
      :aria-label="t('home.carouselPrevious')"
      @click="previous"
    >
      <ChevronLeft :size="20" />
    </button>
    <button
      v-if="slides.length > 1"
      type="button"
      class="carousel-arrow next"
      :aria-label="t('home.carouselNext')"
      @click="next"
    >
      <ChevronRight :size="20" />
    </button>
    <footer v-if="slides.length > 1">
      <div>
        <button
          v-for="(slide, index) in slides"
          :key="slide.id"
          type="button"
          :class="{ active: index === current }"
          :aria-label="
            t('home.carouselGoTo', { index: index + 1, title: slide.title })
          "
          :aria-current="index === current ? 'true' : undefined"
          @click="go(index)"
        ></button>
      </div>
      <span
        >{{ String(current + 1).padStart(2, "0") }} /
        {{ String(slides.length).padStart(2, "0") }}</span
      >
      <button
        type="button"
        :aria-label="paused ? t('home.carouselPlay') : t('home.carouselPause')"
        @click="togglePaused"
      >
        <Play v-if="paused" :size="14" /><Pause v-else :size="14" />
      </button>
    </footer>
  </section>
</template>

<style scoped>
.home-carousel {
  position: relative;
  overflow: hidden;
  border-radius: 12px;
  outline: none;
  touch-action: pan-y;
}
.home-carousel:focus-visible {
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--text) 24%, transparent);
}
.carousel-arrow {
  position: absolute;
  z-index: 5;
  top: 50%;
  width: 44px;
  height: 44px;
  display: grid;
  place-items: center;
  transform: translateY(-50%);
  border: 1px solid rgb(255 255 255 / 0.35);
  border-radius: 50%;
  background: rgb(0 0 0 / 0.46);
  color: #fff;
  backdrop-filter: blur(9px);
  cursor: pointer;
}
.carousel-arrow.previous {
  left: 18px;
}
.carousel-arrow.next {
  right: 18px;
}
.home-carousel > footer {
  position: absolute;
  z-index: 5;
  left: 50%;
  bottom: 15px;
  display: flex;
  align-items: center;
  gap: 12px;
  transform: translateX(-50%);
  padding: 7px 10px;
  border: 1px solid rgb(255 255 255 / 0.22);
  border-radius: 999px;
  background: rgb(0 0 0 / 0.5);
  color: #fff;
  backdrop-filter: blur(10px);
}
.home-carousel > footer > div {
  display: flex;
  gap: 5px;
}
.home-carousel > footer button {
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.home-carousel > footer > div button {
  width: 7px;
  height: 7px;
  padding: 0;
  border-radius: 999px;
  background: rgb(255 255 255 / 0.46);
  transition:
    width 0.2s ease,
    background 0.2s ease;
}
.home-carousel > footer > div button.active {
  width: 24px;
  background: #fff;
}
.home-carousel > footer span {
  font-size: 9px;
}
.carousel-fade-enter-active,
.carousel-fade-leave-active {
  transition:
    opacity 0.28s ease,
    transform 0.28s ease;
}
.carousel-fade-enter-from {
  opacity: 0;
  transform: translateX(18px);
}
.carousel-fade-leave-to {
  opacity: 0;
  transform: translateX(-18px);
}
@media (max-width: 620px) {
  .carousel-arrow {
    width: 38px;
    height: 38px;
  }
  .carousel-arrow.previous {
    left: 10px;
  }
  .carousel-arrow.next {
    right: 10px;
  }
  .home-carousel > footer {
    bottom: 10px;
  }
}
</style>
