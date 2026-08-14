package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

type catalogPaymentChannelDTO struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	Code                string    `json:"code"`
	FeeRate             int       `json:"fee_rate"`
	Enabled             bool      `json:"enabled"`
	Sort                int       `json:"sort"`
	SupportedCurrencies []string  `json:"supported_currencies"`
	SettlementCurrency  string    `json:"settlement_currency"`
	CreatedAt           time.Time `json:"created_at,omitempty"`
	UpdatedAt           time.Time `json:"updated_at,omitempty"`
}

type adminCatalogProductDTO struct {
	model.Product
	PaymentChannelIDs []uuid.UUID                `json:"payment_channel_ids"`
	PaymentChannels   []catalogPaymentChannelDTO `json:"payment_channels"`
	Media             []catalogMediaDTO          `json:"media"`
}

type productPaymentAssignmentRow struct {
	ProductID           uuid.UUID       `gorm:"column:product_id"`
	ChannelID           uuid.UUID       `gorm:"column:channel_id"`
	Name                string          `gorm:"column:name"`
	Code                string          `gorm:"column:code"`
	FeeRate             int             `gorm:"column:fee_rate"`
	Enabled             bool            `gorm:"column:enabled"`
	Sort                int             `gorm:"column:sort"`
	SupportedCurrencies json.RawMessage `gorm:"column:supported_currencies"`
	SettlementCurrency  string          `gorm:"column:settlement_currency"`
	CreatedAt           time.Time       `gorm:"column:created_at"`
	UpdatedAt           time.Time       `gorm:"column:updated_at"`
}

func toCatalogPaymentChannelDTO(channel model.PaymentChannel) catalogPaymentChannelDTO {
	currencies, _ := paymentChannelCurrencies(channel)
	return catalogPaymentChannelDTO{
		ID: channel.ID, Name: channel.Name, Code: channel.Code, FeeRate: channel.FeeRate,
		Enabled: channel.Enabled, Sort: channel.Sort, SupportedCurrencies: currencies, SettlementCurrency: channel.SettlementCurrency, CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt,
	}
}

func parseCatalogPaymentChannelIDs(values []string) ([]uuid.UUID, error) {
	if len(values) > 100 {
		return nil, errCatalogInvalidRequest
	}
	result := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for _, raw := range values {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || parsed == uuid.Nil {
			return nil, errCatalogInvalidRequest
		}
		if _, duplicate := seen[parsed]; duplicate {
			return nil, errCatalogInvalidRequest
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result, nil
}

func parsePublicPaymentChannelProductIDs(c *gin.Context) ([]uuid.UUID, error) {
	rawValues := make([]string, 0, 2)
	if value := strings.TrimSpace(c.Query("product_id")); value != "" {
		rawValues = append(rawValues, value)
	}
	if value := strings.TrimSpace(c.Query("product_ids")); value != "" {
		rawValues = append(rawValues, strings.Split(value, ",")...)
	}
	if len(rawValues) > 20 {
		return nil, errCatalogInvalidRequest
	}
	result := make([]uuid.UUID, 0, len(rawValues))
	seen := make(map[uuid.UUID]struct{}, len(rawValues))
	for _, raw := range rawValues {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || parsed == uuid.Nil {
			return nil, errCatalogInvalidRequest
		}
		if _, exists := seen[parsed]; exists {
			continue
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result, nil
}

func ensureCatalogPaymentChannels(tx *gorm.DB, channelIDs []uuid.UUID) error {
	if len(channelIDs) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.PaymentChannel{}).Where("id IN ? AND enabled = ?", channelIDs, true).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(channelIDs)) {
		return errCatalogPaymentChannel
	}
	return nil
}

func replaceProductPaymentChannels(tx *gorm.DB, productID uuid.UUID, channelIDs []uuid.UUID) error {
	if err := tx.Where("product_id = ?", productID).Delete(&model.ProductPaymentChannel{}).Error; err != nil {
		return err
	}
	if len(channelIDs) == 0 {
		return nil
	}
	items := make([]model.ProductPaymentChannel, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		items = append(items, model.ProductPaymentChannel{ProductID: productID, ChannelID: channelID})
	}
	return tx.Create(&items).Error
}

func adminCatalogProductDTOs(db *gorm.DB, products []model.Product) ([]adminCatalogProductDTO, error) {
	result := make([]adminCatalogProductDTO, 0, len(products))
	if len(products) == 0 {
		return result, nil
	}
	productIDs := make([]uuid.UUID, 0, len(products))
	for _, product := range products {
		productIDs = append(productIDs, product.ID)
	}
	var rows []productPaymentAssignmentRow
	if err := db.Table("product_payment_channels ppc").
		Select("ppc.product_id, pc.id AS channel_id, pc.name, pc.code, pc.fee_rate, pc.enabled, pc.sort, pc.supported_currencies, pc.settlement_currency, pc.created_at, pc.updated_at").
		Joins("JOIN payment_channels pc ON pc.id = ppc.channel_id AND pc.deleted_at IS NULL").
		Where("ppc.product_id IN ?", productIDs).
		Order("ppc.product_id, pc.sort DESC, pc.created_at ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	idsByProduct := make(map[uuid.UUID][]uuid.UUID, len(products))
	channelsByProduct := make(map[uuid.UUID][]catalogPaymentChannelDTO, len(products))
	for _, row := range rows {
		currencies, _ := paymentChannelCurrencies(model.PaymentChannel{
			SupportedCurrencies: row.SupportedCurrencies,
			SettlementCurrency:  row.SettlementCurrency,
		})
		idsByProduct[row.ProductID] = append(idsByProduct[row.ProductID], row.ChannelID)
		channelsByProduct[row.ProductID] = append(channelsByProduct[row.ProductID], catalogPaymentChannelDTO{
			ID: row.ChannelID, Name: row.Name, Code: row.Code, FeeRate: row.FeeRate,
			Enabled: row.Enabled, Sort: row.Sort, SupportedCurrencies: currencies, SettlementCurrency: row.SettlementCurrency, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	mediaByProduct, err := catalogMediaForOwners(db, "product", productIDs)
	if err != nil {
		return nil, err
	}
	for _, product := range products {
		ids := idsByProduct[product.ID]
		channels := channelsByProduct[product.ID]
		if ids == nil {
			ids = []uuid.UUID{}
		}
		if channels == nil {
			channels = []catalogPaymentChannelDTO{}
		}
		result = append(result, adminCatalogProductDTO{Product: product, PaymentChannelIDs: ids, PaymentChannels: channels, Media: mediaByProduct[product.ID]})
	}
	return result, nil
}

func availablePaymentChannelsForProducts(db *gorm.DB, productIDs []uuid.UUID, requestedCurrency ...string) ([]model.PaymentChannel, error) {
	var channels []model.PaymentChannel
	if err := db.Select("id", "name", "code", "provider", "fee_rate", "enabled", "sort", "supported_currencies", "settlement_currency").Where("enabled = ?", true).Order("sort DESC, created_at ASC").Find(&channels).Error; err != nil {
		return nil, err
	}
	if len(productIDs) == 0 || len(channels) == 0 {
		return channels, nil
	}
	uniqueProducts := make(map[uuid.UUID]struct{}, len(productIDs))
	for _, productID := range productIDs {
		if productID != uuid.Nil {
			uniqueProducts[productID] = struct{}{}
		}
	}
	orderedProducts := make([]uuid.UUID, 0, len(uniqueProducts))
	for productID := range uniqueProducts {
		orderedProducts = append(orderedProducts, productID)
	}
	if len(orderedProducts) == 0 {
		return []model.PaymentChannel{}, nil
	}
	var products []model.Product
	if err := db.Select("id", "currency").Where("id IN ?", orderedProducts).Find(&products).Error; err != nil {
		return nil, err
	}
	if len(products) != len(orderedProducts) {
		return []model.PaymentChannel{}, nil
	}
	var assignments []model.ProductPaymentChannel
	if err := db.Where("product_id IN ?", orderedProducts).Find(&assignments).Error; err != nil {
		return nil, err
	}
	restricted := make(map[uuid.UUID]bool, len(orderedProducts))
	allowed := make(map[uuid.UUID]map[uuid.UUID]bool, len(orderedProducts))
	for _, assignment := range assignments {
		restricted[assignment.ProductID] = true
		if allowed[assignment.ProductID] == nil {
			allowed[assignment.ProductID] = make(map[uuid.UUID]bool)
		}
		allowed[assignment.ProductID][assignment.ChannelID] = true
	}
	result := make([]model.PaymentChannel, 0, len(channels))
	for _, channel := range channels {
		permitted := true
		for _, productID := range orderedProducts {
			if !paymentChannelSupportsCurrency(channel, channel.SettlementCurrency) || (restricted[productID] && !allowed[productID][channel.ID]) {
				permitted = false
				break
			}
		}
		if permitted {
			result = append(result, channel)
		}
	}
	return result, nil
}

func (h Handler) AdminCatalogPaymentChannels(c *gin.Context) {
	var channels []model.PaymentChannel
	err := h.DB.Select("id", "name", "code", "fee_rate", "enabled", "sort", "supported_currencies", "settlement_currency", "created_at", "updated_at").Order("enabled DESC, sort DESC, created_at ASC").Find(&channels).Error
	if err != nil {
		response.Error(c, 500, 50063, "error.payment_channel_fetch_failed")
		return
	}
	items := make([]catalogPaymentChannelDTO, 0, len(channels))
	for _, channel := range channels {
		items = append(items, toCatalogPaymentChannelDTO(channel))
	}
	response.OK(c, items)
}

func singleAdminCatalogProductDTO(db *gorm.DB, product model.Product) (adminCatalogProductDTO, error) {
	items, err := adminCatalogProductDTOs(db, []model.Product{product})
	if err != nil {
		return adminCatalogProductDTO{}, err
	}
	if len(items) != 1 {
		return adminCatalogProductDTO{}, errors.New("product DTO hydration failed")
	}
	return items[0], nil
}
