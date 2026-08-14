import type { Category, Product } from "../types";
import { safePublicHTTPURL } from "./publicUrl";

const categoryArt = [
  "/assets/brand/linlinqi-category-gaming.webp",
  "/assets/brand/linlinqi-category-software.webp",
  "/assets/brand/linlinqi-category-giftcard.webp",
  "/assets/brand/linlinqi-category-membership.webp",
  "/assets/brand/linlinqi-category-telecom.webp",
  "/assets/brand/linlinqi-category-streaming.webp",
  "/assets/brand/linlinqi-category-cloud.webp",
  "/assets/brand/linlinqi-category-security.webp",
  "/assets/brand/linlinqi-category-education.webp",
  "/assets/brand/linlinqi-category-entertainment.webp",
] as const;

const keywordArt: Array<[string[], string]> = [
  [["game", "gaming", "steam", "游戏", "点卡"], categoryArt[0]],
  [["software", "license", "key", "软件", "授权"], categoryArt[1]],
  [["gift", "card", "礼品", "购物卡"], categoryArt[2]],
  [["member", "subscription", "vip", "会员", "订阅"], categoryArt[3]],
  [["telecom", "mobile", "phone", "通信", "话费", "充值"], categoryArt[4]],
  [["stream", "video", "music", "流媒体", "影音"], categoryArt[5]],
  [["cloud", "server", "hosting", "云", "主机"], categoryArt[6]],
  [["security", "vpn", "网络安全", "安全"], categoryArt[7]],
  [["education", "course", "learning", "教育", "课程"], categoryArt[8]],
  [["entertainment", "digital", "娱乐", "数字"], categoryArt[9]],
];

const productCarouselArt: Record<string, string[]> = {
  "linlinqi-starlight-game-credit": [
    "/assets/brand/linlinqi-game-credit-carousel-01.webp",
    "/assets/brand/linlinqi-game-credit-carousel-02.webp",
    "/assets/brand/linlinqi-game-credit-carousel-03.webp",
  ],
};

function stableIndex(value: string) {
  return [...value].reduce(
    (total, character) => total + character.codePointAt(0)!,
    0,
  );
}

export function categoryArtwork(category?: Partial<Category> | null) {
  const configuredImage = safePublicHTTPURL(category?.image_url);
  if (configuredImage) return configuredImage;
  const identity =
    `${category?.slug || ""} ${category?.name || ""}`.toLowerCase();
  const matched = keywordArt.find(([keywords]) =>
    keywords.some((keyword) => identity.includes(keyword)),
  );
  return (
    matched?.[1] || categoryArt[stableIndex(identity) % categoryArt.length]
  );
}

export function productArtwork(product?: Partial<Product> | null) {
  return (
    safePublicHTTPURL(product?.cover_url) ||
    safePublicHTTPURL(
      product?.media?.find((item) => item.role === "cover")?.url,
    ) ||
    safePublicHTTPURL(product?.media?.[0]?.url) ||
    (product?.category
      ? categoryArtwork(product.category)
      : "/assets/brand/linlinqi-product-universal.webp")
  );
}

export function productArtworkGallery(product?: Partial<Product> | null) {
  const curatedGallery = productCarouselArt[product?.slug || ""] || [];
  const candidates = [
    product?.cover_url,
    ...(product?.media || []).map((item) => item.url),
    ...curatedGallery,
    ...(curatedGallery.length
      ? []
      : [
          product?.category?.image_url,
          product?.category
            ? categoryArtwork(product.category)
            : "/assets/brand/linlinqi-product-universal.webp",
        ]),
  ];
  return [
    ...new Set(
      candidates
        .map(safePublicHTTPURL)
        .filter((value): value is string => Boolean(value)),
    ),
  ];
}
