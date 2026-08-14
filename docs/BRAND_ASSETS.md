# LinLinQi 品牌图片资产

LinLinQi 前台品牌位图位于 `user/public/assets/brand/`。这些图片为本项目独立生成，不使用或复刻 Dujiao、Dujiao Next 及其他第三方仓库的代码、样式、商标或素材。

## 运行时使用规则

- 商品上传的 `cover_url` 与媒体库 `cover/gallery/detail` 始终优先于内置图片。
- 分类上传的 `image_url` 优先于内置分类图片。
- 内置图片只作为空数据、旧数据或尚未配置媒体时的安全视觉回退，不伪造商品或业务记录。
- 所有分类图均采用无文字构图，避免语言切换后出现不可翻译文字。
- 前台使用 `loading="lazy"`、`decoding="async"`；首页首屏 Hero 使用高优先级加载。

## 文件清单

- `linlinqi-hero-commerce.webp`：商城首页宽屏 Hero。
- `linlinqi-hero-reseller.webp`：经销商与企业采购 Hero。
- `linlinqi-product-universal.webp`：无分类商品的通用数字交付回退图。
- `linlinqi-category-gaming.webp`：游戏与点卡。
- `linlinqi-category-software.webp`：软件与授权。
- `linlinqi-category-giftcard.webp`：礼品卡。
- `linlinqi-category-membership.webp`：会员订阅。
- `linlinqi-category-telecom.webp`：通信充值。
- `linlinqi-category-streaming.webp`：流媒体。
- `linlinqi-category-cloud.webp`：云服务。
- `linlinqi-category-security.webp`：网络安全。
- `linlinqi-category-education.webp`：在线教育。
- `linlinqi-category-entertainment.webp`：数字娱乐。

生产静态目录仅发布 WebP，避免生成母版拖慢首屏。无损 PNG 母版保存在项目根目录 `assets/brand-source/`，不进入 Vite 产物。

## 运营替换

运营人员应优先在后台“商品与分类 → 媒体”中上传真实商品图片。服务端按 SHA-256 内容寻址保存文件，并通过 `/media/sha256/...` 提供不可变缓存；远程供货图片可按供应映射的 `auto_sync_media` 与 `mirror_remote_media` 策略同步到本地。

生成式素材上线前仍应由运营人员完成品牌、地域和行业合规复核。禁止把第三方商标、平台界面、人物肖像或受限制素材加入默认资产。
