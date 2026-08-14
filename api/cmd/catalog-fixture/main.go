// Command catalog-fixture installs an idempotent, clearly fictional catalog
// for local LinLinQi validation. It deliberately creates no users,
// administrators, API credentials, supplier credentials, or real-value keys.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

const cardsPerVariant = 32

var errDryRunRollback = errors.New("catalog fixture dry-run rollback")

type categorySpec struct {
	Name        string
	Slug        string
	Description string
	Icon        string
	ImageURL    string
	Sort        int
}

type variantSpec struct {
	SKU           string
	Name          string
	Attributes    map[string]string
	Price         int64
	ComparePrice  int64
	CostPrice     int64
	Sort          int
	PurchaseLimit int
}

type productSpec struct {
	CategorySlug string
	Name         string
	Slug         string
	Summary      string
	Description  string
	CoverURL     string
	Price        int64
	ComparePrice int64
	CostPrice    int64
	Sort         int
	Featured     bool
	Tags         string
	Variants     []variantSpec
}

type fixtureStats struct {
	Mode                     string `json:"mode"`
	CategoriesCreated        int    `json:"categories_created"`
	CategoriesExisting       int    `json:"categories_existing"`
	ProductsCreated          int    `json:"products_created"`
	ProductsExisting         int    `json:"products_existing"`
	VariantsCreated          int    `json:"variants_created"`
	VariantsExisting         int    `json:"variants_existing"`
	InventoryBatchesCreated  int    `json:"inventory_batches_created"`
	InventoryBatchesExisting int    `json:"inventory_batches_existing"`
	CardsCreated             int    `json:"cards_created"`
	CardsExisting            int    `json:"cards_existing"`
	SandboxChannelCreated    bool   `json:"sandbox_channel_created"`
	SandboxChannelExisting   bool   `json:"sandbox_channel_existing"`
	SandboxCurrenciesUpdated bool   `json:"sandbox_currencies_updated"`
	SandboxSkipped           bool   `json:"sandbox_skipped"`
	ProductChannelsCreated   int    `json:"product_channels_created"`
	ProductChannelsExisting  int    `json:"product_channels_existing"`
}

var categories = []categorySpec{
	{Name: "游戏与点数", Slug: "linlinqi-gaming", Description: "游戏点数、通行证与数字兑换内容。", Icon: "gamepad", ImageURL: "/assets/brand/linlinqi-category-gaming.webp", Sort: 500},
	{Name: "软件与授权", Slug: "linlinqi-software", Description: "办公、创作和开发工具的数字授权。", Icon: "code", ImageURL: "/assets/brand/linlinqi-category-software.webp", Sort: 490},
	{Name: "会员与订阅", Slug: "linlinqi-membership", Description: "周期会员、数字权益和订阅服务。", Icon: "crown", ImageURL: "/assets/brand/linlinqi-category-membership.webp", Sort: 480},
	{Name: "云服务", Slug: "linlinqi-cloud", Description: "云端资源、存储空间与 API 用量包。", Icon: "cloud", ImageURL: "/assets/brand/linlinqi-category-cloud.webp", Sort: 470},
	{Name: "安全工具", Slug: "linlinqi-security", Description: "隐私保护、设备安全与开发测试工具。", Icon: "shield", ImageURL: "/assets/brand/linlinqi-category-security.webp", Sort: 460},
	{Name: "学习与成长", Slug: "linlinqi-education", Description: "数字课程、题库和学习会员。", Icon: "book", ImageURL: "/assets/brand/linlinqi-category-education.webp", Sort: 450},
}

var products = []productSpec{
	{CategorySlug: "linlinqi-gaming", Name: "LinLinQi 星河游戏点数卡", Slug: "linlinqi-starlight-game-credit", Summary: "秒级自动交付的虚构测试点数卡", Description: "用于 LinLinQi 本地订单、库存并发与自动发货验证。卡密仅为虚构测试数据，不具备任何外部平台价值。", CoverURL: "/assets/brand/linlinqi-game-credit-carousel-01.webp", Price: 990, ComparePrice: 1290, CostPrice: 500, Sort: 800, Featured: true, Tags: "自动发货,测试商品,游戏", Variants: []variantSpec{
		{SKU: "LQ-GAME-100", Name: "100 点", Attributes: map[string]string{"面值": "100 点", "区域": "全球测试区"}, Price: 990, ComparePrice: 1290, CostPrice: 500, Sort: 20, PurchaseLimit: 10},
		{SKU: "LQ-GAME-500", Name: "500 点", Attributes: map[string]string{"面值": "500 点", "区域": "全球测试区"}, Price: 3990, ComparePrice: 4990, CostPrice: 2500, Sort: 10, PurchaseLimit: 5},
	}},
	{CategorySlug: "linlinqi-gaming", Name: "LinLinQi 极光通行证", Slug: "linlinqi-aurora-pass", Summary: "多周期规格与库存隔离测试", Description: "用于验证规格定价、限购和并发扣减。所有兑换内容均为 LinLinQi 虚构测试数据。", CoverURL: "/assets/brand/linlinqi-category-entertainment.webp", Price: 1590, ComparePrice: 1990, CostPrice: 800, Sort: 790, Featured: false, Tags: "自动发货,测试商品,通行证", Variants: []variantSpec{
		{SKU: "LQ-PASS-30D", Name: "30 天", Attributes: map[string]string{"周期": "30 天", "区域": "测试区"}, Price: 1590, ComparePrice: 1990, CostPrice: 800, Sort: 20, PurchaseLimit: 5},
		{SKU: "LQ-PASS-90D", Name: "90 天", Attributes: map[string]string{"周期": "90 天", "区域": "测试区"}, Price: 3990, ComparePrice: 4990, CostPrice: 2200, Sort: 10, PurchaseLimit: 3},
	}},
	{CategorySlug: "linlinqi-software", Name: "LinLinQi 工作台专业版", Slug: "linlinqi-workspace-pro", Summary: "办公协作授权交付测试", Description: "覆盖软件授权、不同席位规格和卡密展示流程。授权码仅供本地测试。", CoverURL: "/assets/brand/linlinqi-category-software.webp", Price: 1990, ComparePrice: 2590, CostPrice: 1000, Sort: 780, Featured: true, Tags: "自动发货,测试商品,软件", Variants: []variantSpec{
		{SKU: "LQ-WORK-1U", Name: "单席位月卡", Attributes: map[string]string{"席位": "1", "周期": "30 天"}, Price: 1990, ComparePrice: 2590, CostPrice: 1000, Sort: 20, PurchaseLimit: 5},
		{SKU: "LQ-WORK-5U", Name: "五席位月卡", Attributes: map[string]string{"席位": "5", "周期": "30 天"}, Price: 6990, ComparePrice: 8990, CostPrice: 4000, Sort: 10, PurchaseLimit: 2},
	}},
	{CategorySlug: "linlinqi-software", Name: "LinLinQi 开发者工具包", Slug: "linlinqi-developer-toolkit", Summary: "开发工具数字授权测试包", Description: "用于验证高低价规格、订单快照和售后查询。授权码不对应任何第三方软件。", CoverURL: "/assets/brand/linlinqi-product-universal.webp", Price: 2490, ComparePrice: 3290, CostPrice: 1400, Sort: 770, Featured: false, Tags: "自动发货,测试商品,开发", Variants: []variantSpec{
		{SKU: "LQ-DEV-STD", Name: "标准版", Attributes: map[string]string{"版本": "标准版", "设备": "1 台"}, Price: 2490, ComparePrice: 3290, CostPrice: 1400, Sort: 20, PurchaseLimit: 5},
		{SKU: "LQ-DEV-TEAM", Name: "团队版", Attributes: map[string]string{"版本": "团队版", "设备": "10 台"}, Price: 12900, ComparePrice: 15900, CostPrice: 8000, Sort: 10, PurchaseLimit: 2},
	}},
	{CategorySlug: "linlinqi-membership", Name: "LinLinQi 畅享会员", Slug: "linlinqi-plus-membership", Summary: "周期订阅自动发货测试", Description: "验证会员型数字商品的周期规格与多币种报价。兑换码只在本地测试环境使用。", CoverURL: "/assets/brand/linlinqi-category-membership.webp", Price: 1290, ComparePrice: 1690, CostPrice: 600, Sort: 760, Featured: true, Tags: "自动发货,测试商品,会员", Variants: []variantSpec{
		{SKU: "LQ-PLUS-1M", Name: "月度会员", Attributes: map[string]string{"周期": "1 个月", "权益": "标准"}, Price: 1290, ComparePrice: 1690, CostPrice: 600, Sort: 20, PurchaseLimit: 10},
		{SKU: "LQ-PLUS-1Y", Name: "年度会员", Attributes: map[string]string{"周期": "12 个月", "权益": "标准"}, Price: 9900, ComparePrice: 12900, CostPrice: 6000, Sort: 10, PurchaseLimit: 3},
	}},
	{CategorySlug: "linlinqi-cloud", Name: "LinLinQi 云端实验额度", Slug: "linlinqi-cloud-lab-credit", Summary: "云端资源额度自动交付测试", Description: "用于验证金额、库存和支付成功后的自动交付链路，不连接任何真实云厂商。", CoverURL: "/assets/brand/linlinqi-category-cloud.webp", Price: 2990, ComparePrice: 3590, CostPrice: 1800, Sort: 750, Featured: true, Tags: "自动发货,测试商品,云服务", Variants: []variantSpec{
		{SKU: "LQ-CLOUD-50", Name: "50 元测试额度", Attributes: map[string]string{"额度": "50", "有效期": "30 天"}, Price: 2990, ComparePrice: 3590, CostPrice: 1800, Sort: 20, PurchaseLimit: 5},
		{SKU: "LQ-CLOUD-200", Name: "200 元测试额度", Attributes: map[string]string{"额度": "200", "有效期": "90 天"}, Price: 9990, ComparePrice: 11900, CostPrice: 7000, Sort: 10, PurchaseLimit: 3},
	}},
	{CategorySlug: "linlinqi-security", Name: "LinLinQi 安全巡检订阅", Slug: "linlinqi-security-inspection", Summary: "安全服务订阅交付测试", Description: "验证安全类数字订阅的下单和交付边界。兑换码为虚构测试数据，不提供真实安全服务。", CoverURL: "/assets/brand/linlinqi-category-security.webp", Price: 1590, ComparePrice: 2190, CostPrice: 900, Sort: 740, Featured: false, Tags: "自动发货,测试商品,安全", Variants: []variantSpec{
		{SKU: "LQ-SEC-BASIC", Name: "基础巡检", Attributes: map[string]string{"方案": "基础", "周期": "30 天"}, Price: 1590, ComparePrice: 2190, CostPrice: 900, Sort: 20, PurchaseLimit: 5},
		{SKU: "LQ-SEC-PRO", Name: "专业巡检", Attributes: map[string]string{"方案": "专业", "周期": "30 天"}, Price: 4990, ComparePrice: 6290, CostPrice: 3200, Sort: 10, PurchaseLimit: 3},
	}},
	{CategorySlug: "linlinqi-education", Name: "LinLinQi 学习成长包", Slug: "linlinqi-learning-pack", Summary: "课程权益兑换交付测试", Description: "用于验证教育类数字商品的库存、规格和订单流程，不对应任何第三方课程。", CoverURL: "/assets/brand/linlinqi-category-education.webp", Price: 1890, ComparePrice: 2390, CostPrice: 1000, Sort: 730, Featured: false, Tags: "自动发货,测试商品,教育", Variants: []variantSpec{
		{SKU: "LQ-LEARN-30D", Name: "30 天学习包", Attributes: map[string]string{"周期": "30 天", "等级": "入门"}, Price: 1890, ComparePrice: 2390, CostPrice: 1000, Sort: 20, PurchaseLimit: 5},
		{SKU: "LQ-LEARN-180D", Name: "180 天学习包", Attributes: map[string]string{"周期": "180 天", "等级": "进阶"}, Price: 7990, ComparePrice: 9990, CostPrice: 5000, Sort: 10, PurchaseLimit: 3},
	}},
}

func main() {
	apply := flag.Bool("apply", false, "commit the fixture transaction (without this flag all work is rolled back)")
	flag.Parse()
	if flag.NArg() != 0 {
		slog.Error("catalog fixture stopped", "error", "unexpected positional arguments")
		os.Exit(2)
	}

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		fail(fmt.Errorf("configuration: %w", err))
	}
	vault, err := security.NewVault(cfg.DataEncryptionKey)
	if err != nil {
		fail(fmt.Errorf("vault: %w", err))
	}
	resources, err := database.Connect(cfg)
	if err != nil {
		fail(err)
	}
	// A fixture can touch hundreds of encrypted rows. Keep command output to the
	// audited aggregate summary instead of emitting ciphertext and nonce values
	// through the development SQL logger.
	resources.DB = resources.DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	if resources.Redis != nil {
		defer resources.Redis.Close()
	}
	if sqlDB, dbErr := resources.DB.DB(); dbErr == nil {
		defer sqlDB.Close()
	}

	stats, err := installFixture(resources.DB, vault, cfg.Env, !*apply)
	if err != nil {
		fail(err)
	}
	payload, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(payload))
}

func fail(err error) {
	slog.Error("catalog fixture stopped", "error", err)
	os.Exit(1)
}

func installFixture(db *gorm.DB, vault *security.Vault, environment string, dryRun bool) (fixtureStats, error) {
	stats := fixtureStats{Mode: "apply"}
	if dryRun {
		stats.Mode = "dry-run"
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "linlinqi-catalog-fixture-v1").Error; err != nil {
			return fmt.Errorf("lock catalog fixture: %w", err)
		}
		categoryIDs, err := ensureCategories(tx, &stats)
		if err != nil {
			return err
		}
		channelID, channelEnabled, err := ensureSandboxChannel(tx, environment, &stats)
		if err != nil {
			return err
		}
		if err := ensureProducts(tx, vault, categoryIDs, channelID, channelEnabled, &stats); err != nil {
			return err
		}
		if dryRun {
			return errDryRunRollback
		}
		return nil
	})
	if dryRun && errors.Is(err, errDryRunRollback) {
		return stats, nil
	}
	if err != nil {
		return fixtureStats{}, fmt.Errorf("install catalog fixture: %w", err)
	}
	return stats, nil
}

func ensureCategories(tx *gorm.DB, stats *fixtureStats) (map[string]uuid.UUID, error) {
	ids := make(map[string]uuid.UUID, len(categories))
	for _, spec := range categories {
		var item model.Category
		err := tx.Where("slug = ?", spec.Slug).First(&item).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			item = model.Category{Name: spec.Name, Slug: spec.Slug, Description: spec.Description, Icon: spec.Icon, ImageURL: spec.ImageURL, Sort: spec.Sort, Enabled: true}
			if err := tx.Create(&item).Error; err != nil {
				return nil, fmt.Errorf("create category %s: %w", spec.Slug, err)
			}
			stats.CategoriesCreated++
		case err != nil:
			return nil, fmt.Errorf("read category %s: %w", spec.Slug, err)
		default:
			if item.Name != spec.Name {
				return nil, fmt.Errorf("category slug %s belongs to %q; refusing to overwrite", spec.Slug, item.Name)
			}
			stats.CategoriesExisting++
		}
		ids[spec.Slug] = item.ID
	}
	return ids, nil
}

func ensureSandboxChannel(tx *gorm.DB, environment string, stats *fixtureStats) (*uuid.UUID, bool, error) {
	if !shouldInstallSandbox(environment) {
		stats.SandboxSkipped = true
		return nil, false, nil
	}
	var currencies []string
	if err := tx.Model(&model.CurrencyDefinition{}).Where("enabled = ?", true).Order("code").Pluck("code", &currencies).Error; err != nil {
		return nil, false, fmt.Errorf("list enabled currencies: %w", err)
	}
	if len(currencies) == 0 {
		currencies = []string{"CNY", "JPY", "USD"}
	}
	for index := range currencies {
		currencies[index] = strings.ToUpper(strings.TrimSpace(currencies[index]))
	}
	sort.Strings(currencies)
	settlementCurrency := "CNY"
	var setting model.Setting
	if err := tx.Select("value").Where("key = ?", "store_currency").First(&setting).Error; err == nil {
		settlementCurrency = strings.ToUpper(strings.TrimSpace(setting.Value))
	}
	if !slices.Contains(currencies, settlementCurrency) {
		settlementCurrency = currencies[0]
	}
	currencyJSON, err := json.Marshal(currencies)
	if err != nil {
		return nil, false, err
	}

	var channel model.PaymentChannel
	err = tx.Where("code = ?", "sandbox").First(&channel).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		channel = model.PaymentChannel{Name: "LinLinQi 本地安全沙箱", Code: "sandbox", Provider: "sandbox", FeeRate: 0, Enabled: true, Sort: 1000, SupportedCurrencies: currencyJSON, SettlementCurrency: settlementCurrency}
		if err := tx.Create(&channel).Error; err != nil {
			return nil, false, fmt.Errorf("create sandbox payment channel: %w", err)
		}
		stats.SandboxChannelCreated = true
	case err != nil:
		return nil, false, fmt.Errorf("read sandbox payment channel: %w", err)
	default:
		if channel.Provider != "sandbox" {
			return nil, false, fmt.Errorf("payment channel code sandbox belongs to provider %q; refusing to overwrite", channel.Provider)
		}
		if len(channel.ConfigCipher) != 0 || len(channel.ConfigNonce) != 0 {
			return nil, false, errors.New("sandbox payment channel unexpectedly contains encrypted configuration; refusing to use it")
		}
		if string(channel.SupportedCurrencies) != string(currencyJSON) {
			if err := tx.Model(&channel).Update("supported_currencies", currencyJSON).Error; err != nil {
				return nil, false, fmt.Errorf("update sandbox payment currencies: %w", err)
			}
			channel.SupportedCurrencies = currencyJSON
			stats.SandboxCurrenciesUpdated = true
		}
		if channel.SettlementCurrency != settlementCurrency {
			if err := tx.Model(&channel).Update("settlement_currency", settlementCurrency).Error; err != nil {
				return nil, false, fmt.Errorf("update sandbox settlement currency: %w", err)
			}
			channel.SettlementCurrency = settlementCurrency
		}
		stats.SandboxChannelExisting = true
	}
	return &channel.ID, channel.Enabled, nil
}

func shouldInstallSandbox(environment string) bool {
	return !strings.EqualFold(strings.TrimSpace(environment), "production")
}

func ensureProducts(tx *gorm.DB, vault *security.Vault, categoryIDs map[string]uuid.UUID, channelID *uuid.UUID, channelEnabled bool, stats *fixtureStats) error {
	for _, spec := range products {
		categoryID, ok := categoryIDs[spec.CategorySlug]
		if !ok {
			return fmt.Errorf("product %s references unknown fixture category %s", spec.Slug, spec.CategorySlug)
		}
		product, err := ensureProduct(tx, categoryID, spec, stats)
		if err != nil {
			return err
		}
		if channelID != nil && channelEnabled {
			var assignment model.ProductPaymentChannel
			err := tx.Where("product_id = ? AND channel_id = ?", product.ID, *channelID).First(&assignment).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				assignment = model.ProductPaymentChannel{ProductID: product.ID, ChannelID: *channelID}
				if err := tx.Create(&assignment).Error; err != nil {
					return fmt.Errorf("assign sandbox channel to product %s: %w", spec.Slug, err)
				}
				stats.ProductChannelsCreated++
			case err != nil:
				return fmt.Errorf("read sandbox channel assignment for product %s: %w", spec.Slug, err)
			default:
				stats.ProductChannelsExisting++
			}
		}
		for _, variant := range spec.Variants {
			item, err := ensureVariant(tx, product.ID, spec.Slug, variant, stats)
			if err != nil {
				return err
			}
			if err := ensureVariantInventory(tx, vault, product, item, stats); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureProduct(tx *gorm.DB, categoryID uuid.UUID, spec productSpec, stats *fixtureStats) (model.Product, error) {
	var item model.Product
	err := tx.Where("slug = ?", spec.Slug).First(&item).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		item = model.Product{CategoryID: categoryID, Name: spec.Name, Slug: spec.Slug, Summary: spec.Summary, Description: spec.Description, CoverURL: spec.CoverURL, Currency: "CNY", Price: spec.Price, ComparePrice: spec.ComparePrice, CostPrice: spec.CostPrice, DeliveryType: "auto", InventoryMode: "local", Status: "on_sale", Sort: spec.Sort, Featured: spec.Featured, Tags: spec.Tags}
		if err := tx.Create(&item).Error; err != nil {
			return model.Product{}, fmt.Errorf("create product %s: %w", spec.Slug, err)
		}
		stats.ProductsCreated++
	case err != nil:
		return model.Product{}, fmt.Errorf("read product %s: %w", spec.Slug, err)
	default:
		if item.Name != spec.Name || item.CategoryID != categoryID {
			return model.Product{}, fmt.Errorf("product slug %s belongs to a different catalog item; refusing to overwrite", spec.Slug)
		}
		stats.ProductsExisting++
	}
	return item, nil
}

func ensureVariant(tx *gorm.DB, productID uuid.UUID, productSlug string, spec variantSpec, stats *fixtureStats) (model.ProductVariant, error) {
	attributes, err := json.Marshal(spec.Attributes)
	if err != nil {
		return model.ProductVariant{}, fmt.Errorf("marshal attributes for %s: %w", spec.SKU, err)
	}
	var item model.ProductVariant
	err = tx.Where("sku = ?", spec.SKU).First(&item).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		item = model.ProductVariant{ProductID: productID, SKU: spec.SKU, Name: spec.Name, Attributes: string(attributes), Price: spec.Price, ComparePrice: spec.ComparePrice, CostPrice: spec.CostPrice, Status: "active", Sort: spec.Sort, PurchaseLimit: spec.PurchaseLimit}
		if err := tx.Create(&item).Error; err != nil {
			return model.ProductVariant{}, fmt.Errorf("create variant %s: %w", spec.SKU, err)
		}
		stats.VariantsCreated++
	case err != nil:
		return model.ProductVariant{}, fmt.Errorf("read variant %s: %w", spec.SKU, err)
	default:
		if item.ProductID != productID || item.Name != spec.Name {
			return model.ProductVariant{}, fmt.Errorf("variant SKU %s belongs to another product instead of %s; refusing to overwrite", spec.SKU, productSlug)
		}
		stats.VariantsExisting++
	}
	return item, nil
}

func ensureVariantInventory(tx *gorm.DB, vault *security.Vault, product model.Product, variant model.ProductVariant, stats *fixtureStats) error {
	batchNo := "LINLINQI-FIXTURE-V1-" + variant.SKU
	var batch model.InventoryBatch
	err := tx.Where("batch_no = ?", batchNo).First(&batch).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		batch = model.InventoryBatch{ProductID: product.ID, VariantID: &variant.ID, BatchNo: batchNo, Source: "manual_import", TotalCount: cardsPerVariant, ValidCount: cardsPerVariant}
		if err := tx.Create(&batch).Error; err != nil {
			return fmt.Errorf("create inventory batch %s: %w", batchNo, err)
		}
		stats.InventoryBatchesCreated++
	case err != nil:
		return fmt.Errorf("read inventory batch %s: %w", batchNo, err)
	default:
		if batch.ProductID != product.ID || batch.VariantID == nil || *batch.VariantID != variant.ID {
			return fmt.Errorf("inventory batch %s belongs to another product or variant; refusing to overwrite", batchNo)
		}
		stats.InventoryBatchesExisting++
	}

	for sequence := 1; sequence <= cardsPerVariant; sequence++ {
		plaintext := fixtureCardSecret(product.Slug, variant.SKU, sequence)
		ciphertext, nonce, fingerprint, err := vault.Encrypt(plaintext, product.ID[:])
		if err != nil {
			return fmt.Errorf("encrypt card for %s: %w", variant.SKU, err)
		}
		var existing model.Card
		err = tx.Select("id").Where("product_id = ? AND fingerprint = ?", product.ID, fingerprint).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			card := model.Card{ProductID: product.ID, VariantID: &variant.ID, EncryptedContent: ciphertext, Nonce: nonce, Fingerprint: fingerprint, Preview: security.SecretPreview(plaintext), Status: "available"}
			if err := tx.Create(&card).Error; err != nil {
				return fmt.Errorf("create card for %s sequence %d: %w", variant.SKU, sequence, err)
			}
			stats.CardsCreated++
		case err != nil:
			return fmt.Errorf("read card for %s sequence %d: %w", variant.SKU, sequence, err)
		default:
			stats.CardsExisting++
		}
	}
	return nil
}

func fixtureCardSecret(productSlug, sku string, sequence int) string {
	return fmt.Sprintf("LINLINQI-LOCAL-TEST:%s:%s:%04d:NO-REAL-VALUE", strings.ToUpper(productSlug), sku, sequence)
}
