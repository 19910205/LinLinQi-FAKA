package handler

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

const (
	analyticsDefaultRange = 30 * 24 * time.Hour
	analyticsMaximumRange = 366 * 24 * time.Hour
	analyticsMaximumHours = 7 * 24 * time.Hour
)

var (
	errAnalyticsRangeInvalid = errors.New("analytics range is invalid")
	errAnalyticsAmountRange  = errors.New("analytics amount exceeds int64")
)

type analyticsRange struct {
	From        time.Time
	To          time.Time
	Granularity string
}

type analyticsSeriesPoint struct {
	Bucket    time.Time `json:"bucket"`
	Gross     int64     `json:"gross_revenue"`
	Refunds   int64     `json:"refunds"`
	Net       int64     `json:"net_revenue"`
	Orders    int64     `json:"orders_created"`
	Paid      int64     `json:"paid_orders"`
	Delivered int64     `json:"delivered_orders"`
	NewUsers  int64     `json:"new_users"`
}

type analyticsMetrics struct {
	GrossRevenue       int64   `json:"gross_revenue"`
	Refunds            int64   `json:"refunds"`
	NetRevenue         int64   `json:"net_revenue"`
	OrdersCreated      int64   `json:"orders_created"`
	PaidOrders         int64   `json:"paid_orders"`
	DeliveredOrders    int64   `json:"delivered_orders"`
	NewUsers           int64   `json:"new_users"`
	AverageOrderValue  int64   `json:"average_order_value"`
	RefundRate         float64 `json:"refund_rate"`
	PaymentSuccessRate float64 `json:"payment_success_rate"`
}

type analyticsProductRow struct {
	ProductID    uuid.UUID `json:"product_id"`
	ProductName  string    `json:"product_name"`
	OrderCount   int64     `json:"order_count"`
	Units        int64     `json:"units"`
	GrossRevenue int64     `json:"gross_revenue"`
}

type analyticsChannelRow struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Attempts     int64   `json:"attempts"`
	Succeeded    int64   `json:"succeeded"`
	PaidOrders   int64   `json:"paid_orders"`
	GrossRevenue int64   `json:"gross_revenue"`
	SuccessRate  float64 `json:"success_rate"`
}

type analyticsFunnel struct {
	Orders         int64   `json:"orders"`
	PaymentStarted int64   `json:"payment_started"`
	Paid           int64   `json:"paid"`
	Delivered      int64   `json:"delivered"`
	PaymentRate    float64 `json:"payment_rate"`
	DeliveryRate   float64 `json:"delivery_rate"`
}

type analyticsAmountBucketRow struct {
	Bucket time.Time
	Amount string
}

type analyticsCountBucketRow struct {
	Bucket time.Time
	Count  int64
}

func parseAnalyticsRange(c *gin.Context, now time.Time) (analyticsRange, error) {
	result := analyticsRange{To: now.UTC(), Granularity: strings.ToLower(strings.TrimSpace(c.DefaultQuery("granularity", "day")))}
	if result.Granularity != "day" && result.Granularity != "hour" {
		return analyticsRange{}, errAnalyticsRangeInvalid
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return analyticsRange{}, errAnalyticsRangeInvalid
		}
		result.To = parsed.UTC()
	}
	result.From = result.To.Add(-analyticsDefaultRange)
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return analyticsRange{}, errAnalyticsRangeInvalid
		}
		result.From = parsed.UTC()
	}
	duration := result.To.Sub(result.From)
	if duration <= 0 || duration > analyticsMaximumRange || (result.Granularity == "hour" && duration > analyticsMaximumHours) {
		return analyticsRange{}, errAnalyticsRangeInvalid
	}
	return result, nil
}

func analyticsBucketExpression(granularity, column string) string {
	unit := "day"
	if granularity == "hour" {
		unit = "hour"
	}
	// The unit and column are selected exclusively from internal constants.
	return fmt.Sprintf("date_trunc('%s', %s AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'", unit, column)
}

func floorAnalyticsBucket(value time.Time, granularity string) time.Time {
	value = value.UTC()
	if granularity == "hour" {
		return value.Truncate(time.Hour)
	}
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func nextAnalyticsBucket(value time.Time, granularity string) time.Time {
	if granularity == "hour" {
		return value.Add(time.Hour)
	}
	return value.AddDate(0, 0, 1)
}

func analyticsBucketKey(value time.Time, granularity string) string {
	return floorAnalyticsBucket(value, granularity).Format(time.RFC3339)
}

func buildAnalyticsSeries(window analyticsRange) ([]analyticsSeriesPoint, map[string]int) {
	points := make([]analyticsSeriesPoint, 0, 32)
	indices := make(map[string]int, 32)
	for bucket := floorAnalyticsBucket(window.From, window.Granularity); bucket.Before(window.To); bucket = nextAnalyticsBucket(bucket, window.Granularity) {
		indices[analyticsBucketKey(bucket, window.Granularity)] = len(points)
		points = append(points, analyticsSeriesPoint{Bucket: bucket})
	}
	return points, indices
}

func parseAnalyticsAmount(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || !value.IsInt64() {
		return 0, errAnalyticsAmountRange
	}
	return value.Int64(), nil
}

func percentage(part, whole int64) float64 {
	if whole <= 0 || part <= 0 {
		return 0
	}
	value := float64(part) * 100 / float64(whole)
	return math.Round(value*100) / 100
}

func addAnalyticsAmount(current, addition int64) (int64, error) {
	if (addition > 0 && current > math.MaxInt64-addition) || (addition < 0 && current < math.MinInt64-addition) {
		return 0, errAnalyticsAmountRange
	}
	return current + addition, nil
}

func loadAnalyticsAmountSeries(db *gorm.DB, table, timestampColumn, amountColumn string, where string, args []any, window analyticsRange) ([]analyticsAmountBucketRow, error) {
	allowed := map[string]bool{
		"orders|paid_at|total":              true,
		"refunds|processed_at|order_amount": true,
	}
	if !allowed[table+"|"+timestampColumn+"|"+amountColumn] {
		return nil, errors.New("unsupported analytics amount series")
	}
	expression := analyticsBucketExpression(window.Granularity, timestampColumn)
	query := fmt.Sprintf("SELECT %s AS bucket, COALESCE(SUM(%s::numeric), 0)::text AS amount FROM %s WHERE deleted_at IS NULL AND %s >= ? AND %s < ?", expression, amountColumn, table, timestampColumn, timestampColumn)
	queryArgs := []any{window.From, window.To}
	if strings.TrimSpace(where) != "" {
		query += " AND " + where
		queryArgs = append(queryArgs, args...)
	}
	query += " GROUP BY 1 ORDER BY 1"
	var rows []analyticsAmountBucketRow
	return rows, db.Raw(query, queryArgs...).Scan(&rows).Error
}

func loadAnalyticsCountSeries(db *gorm.DB, table, timestampColumn, where string, args []any, window analyticsRange) ([]analyticsCountBucketRow, error) {
	allowed := map[string]bool{
		"orders|created_at":   true,
		"orders|paid_at":      true,
		"orders|delivered_at": true,
		"users|created_at":    true,
	}
	if !allowed[table+"|"+timestampColumn] {
		return nil, errors.New("unsupported analytics count series")
	}
	expression := analyticsBucketExpression(window.Granularity, timestampColumn)
	query := fmt.Sprintf("SELECT %s AS bucket, COUNT(*) AS count FROM %s WHERE deleted_at IS NULL AND %s >= ? AND %s < ?", expression, table, timestampColumn, timestampColumn)
	queryArgs := []any{window.From, window.To}
	if strings.TrimSpace(where) != "" {
		query += " AND " + where
		queryArgs = append(queryArgs, args...)
	}
	query += " GROUP BY 1 ORDER BY 1"
	var rows []analyticsCountBucketRow
	return rows, db.Raw(query, queryArgs...).Scan(&rows).Error
}

func applyAnalyticsAmounts(points []analyticsSeriesPoint, indices map[string]int, rows []analyticsAmountBucketRow, granularity string, target func(*analyticsSeriesPoint, int64)) error {
	for _, row := range rows {
		amount, err := parseAnalyticsAmount(row.Amount)
		if err != nil || amount < 0 {
			return errAnalyticsAmountRange
		}
		if index, ok := indices[analyticsBucketKey(row.Bucket, granularity)]; ok {
			target(&points[index], amount)
		}
	}
	return nil
}

func applyAnalyticsCounts(points []analyticsSeriesPoint, indices map[string]int, rows []analyticsCountBucketRow, granularity string, target func(*analyticsSeriesPoint, int64)) {
	for _, row := range rows {
		if index, ok := indices[analyticsBucketKey(row.Bucket, granularity)]; ok {
			target(&points[index], row.Count)
		}
	}
}

// AdminAnalyticsOverview returns operational aggregates only. It never exposes
// customer identifiers, email addresses, card contents, IPs, or provider secrets.
func (h Handler) AdminAnalyticsOverview(c *gin.Context) {
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50095, "error.store_currency_fetch_failed")
		return
	}
	window, err := parseAnalyticsRange(c, time.Now())
	if err != nil {
		response.Error(c, 422, 42295, "error.analysis_time_range_invalid")
		return
	}
	points, indices := buildAnalyticsSeries(window)

	grossRows, err := loadAnalyticsAmountSeries(h.DB, "orders", "paid_at", "total", "currency = ?", []any{currencyCode}, window)
	if err == nil {
		err = applyAnalyticsAmounts(points, indices, grossRows, window.Granularity, func(point *analyticsSeriesPoint, value int64) { point.Gross = value })
	}
	refundRows := []analyticsAmountBucketRow(nil)
	if err == nil {
		refundRows, err = loadAnalyticsAmountSeries(h.DB, "refunds", "processed_at", "order_amount", "status = ? AND order_currency = ?", []any{"succeeded", currencyCode}, window)
	}
	if err == nil {
		err = applyAnalyticsAmounts(points, indices, refundRows, window.Granularity, func(point *analyticsSeriesPoint, value int64) { point.Refunds = value })
	}
	var createdRows, paidRows, deliveredRows, userRows []analyticsCountBucketRow
	if err == nil {
		createdRows, err = loadAnalyticsCountSeries(h.DB, "orders", "created_at", "currency = ?", []any{currencyCode}, window)
	}
	if err == nil {
		paidRows, err = loadAnalyticsCountSeries(h.DB, "orders", "paid_at", "currency = ?", []any{currencyCode}, window)
	}
	if err == nil {
		deliveredRows, err = loadAnalyticsCountSeries(h.DB, "orders", "delivered_at", "currency = ?", []any{currencyCode}, window)
	}
	if err == nil {
		userRows, err = loadAnalyticsCountSeries(h.DB, "users", "created_at", "", nil, window)
	}
	if err != nil {
		response.Error(c, 500, 50095, "error.business_analysis_timeseries_fetch_failed")
		return
	}
	applyAnalyticsCounts(points, indices, createdRows, window.Granularity, func(point *analyticsSeriesPoint, value int64) { point.Orders = value })
	applyAnalyticsCounts(points, indices, paidRows, window.Granularity, func(point *analyticsSeriesPoint, value int64) { point.Paid = value })
	applyAnalyticsCounts(points, indices, deliveredRows, window.Granularity, func(point *analyticsSeriesPoint, value int64) { point.Delivered = value })
	applyAnalyticsCounts(points, indices, userRows, window.Granularity, func(point *analyticsSeriesPoint, value int64) { point.NewUsers = value })

	metrics := analyticsMetrics{}
	for index := range points {
		points[index].Net = points[index].Gross - points[index].Refunds
		metrics.GrossRevenue, err = addAnalyticsAmount(metrics.GrossRevenue, points[index].Gross)
		if err == nil {
			metrics.Refunds, err = addAnalyticsAmount(metrics.Refunds, points[index].Refunds)
		}
		if err == nil {
			metrics.NetRevenue, err = addAnalyticsAmount(metrics.NetRevenue, points[index].Net)
		}
		if err != nil {
			response.Error(c, 500, 50095, "error.business_analysis_amount_unsupported")
			return
		}
		metrics.OrdersCreated += points[index].Orders
		metrics.PaidOrders += points[index].Paid
		metrics.DeliveredOrders += points[index].Delivered
		metrics.NewUsers += points[index].NewUsers
	}
	if metrics.PaidOrders > 0 {
		metrics.AverageOrderValue = metrics.GrossRevenue / metrics.PaidOrders
	}
	metrics.RefundRate = percentage(metrics.Refunds, metrics.GrossRevenue)

	var paymentAttempts, paymentSucceeded int64
	if err := h.DB.Model(&struct{ ID uuid.UUID }{}).Table("payment_intents").Where("deleted_at IS NULL AND created_at >= ? AND created_at < ? AND currency = ?", window.From, window.To, currencyCode).Count(&paymentAttempts).Error; err != nil {
		response.Error(c, 500, 50095, "error.payment_conversion_fetch_failed")
		return
	}
	if err := h.DB.Table("payment_intents").Where("deleted_at IS NULL AND created_at >= ? AND created_at < ? AND currency = ? AND status IN ?", window.From, window.To, currencyCode, []string{"succeeded", "partially_refunded", "refunded"}).Count(&paymentSucceeded).Error; err != nil {
		response.Error(c, 500, 50095, "error.payment_conversion_fetch_failed")
		return
	}
	metrics.PaymentSuccessRate = percentage(paymentSucceeded, paymentAttempts)

	type productDBRow struct {
		ProductID    uuid.UUID
		ProductName  string
		OrderCount   int64
		Units        int64
		GrossRevenue string
	}
	var productRows []productDBRow
	productSQL := `SELECT oi.product_id, MAX(oi.product_name) AS product_name, COUNT(DISTINCT oi.order_id) AS order_count, COALESCE(SUM(oi.quantity), 0) AS units, COALESCE(SUM(oi.unit_price::numeric * oi.quantity), 0)::text AS gross_revenue
		FROM order_items oi JOIN orders o ON o.id = oi.order_id AND o.deleted_at IS NULL
		WHERE oi.deleted_at IS NULL AND o.paid_at >= ? AND o.paid_at < ? AND o.currency = ?
		GROUP BY oi.product_id ORDER BY SUM(oi.unit_price::numeric * oi.quantity) DESC, MAX(oi.product_name) ASC LIMIT 10`
	if err := h.DB.Raw(productSQL, window.From, window.To, currencyCode).Scan(&productRows).Error; err != nil {
		response.Error(c, 500, 50095, "error.product_performance_ranking_fetch_failed")
		return
	}
	products := make([]analyticsProductRow, 0, len(productRows))
	for _, row := range productRows {
		amount, parseErr := parseAnalyticsAmount(row.GrossRevenue)
		if parseErr != nil || amount < 0 {
			response.Error(c, 500, 50095, "error.product_performance_amount_unsupported")
			return
		}
		products = append(products, analyticsProductRow{ProductID: row.ProductID, ProductName: row.ProductName, OrderCount: row.OrderCount, Units: row.Units, GrossRevenue: amount})
	}

	type channelPaymentDBRow struct {
		Code      string
		Name      string
		Attempts  int64
		Succeeded int64
	}
	type channelOrderDBRow struct {
		Code         string
		PaidOrders   int64
		GrossRevenue string
	}
	var channelPayments []channelPaymentDBRow
	channelPaymentSQL := `SELECT pc.code, pc.name, COUNT(pi.id) AS attempts, COUNT(pi.id) FILTER (WHERE pi.status IN ('succeeded', 'partially_refunded', 'refunded')) AS succeeded
		FROM payment_intents pi JOIN payment_channels pc ON pc.id = pi.channel_id AND pc.deleted_at IS NULL
		WHERE pi.deleted_at IS NULL AND pi.created_at >= ? AND pi.created_at < ? AND pi.currency = ?
		GROUP BY pc.code, pc.name ORDER BY COUNT(pi.id) DESC, pc.code ASC`
	if err := h.DB.Raw(channelPaymentSQL, window.From, window.To, currencyCode).Scan(&channelPayments).Error; err != nil {
		response.Error(c, 500, 50095, "error.payment_channel_performance_fetch_failed")
		return
	}
	var channelOrders []channelOrderDBRow
	channelOrderSQL := `SELECT COALESCE(NULLIF(payment_method, ''), 'unknown') AS code, COUNT(*) AS paid_orders, COALESCE(SUM(total::numeric), 0)::text AS gross_revenue
		FROM orders WHERE deleted_at IS NULL AND paid_at >= ? AND paid_at < ? AND currency = ?
		GROUP BY COALESCE(NULLIF(payment_method, ''), 'unknown')`
	if err := h.DB.Raw(channelOrderSQL, window.From, window.To, currencyCode).Scan(&channelOrders).Error; err != nil {
		response.Error(c, 500, 50095, "error.channel_revenue_fetch_failed")
		return
	}
	channelsByCode := make(map[string]*analyticsChannelRow, len(channelPayments)+len(channelOrders))
	channelOrder := make([]string, 0, len(channelPayments)+len(channelOrders))
	for _, row := range channelPayments {
		item := &analyticsChannelRow{Code: row.Code, Name: row.Name, Attempts: row.Attempts, Succeeded: row.Succeeded, SuccessRate: percentage(row.Succeeded, row.Attempts)}
		channelsByCode[row.Code] = item
		channelOrder = append(channelOrder, row.Code)
	}
	for _, row := range channelOrders {
		item := channelsByCode[row.Code]
		if item == nil {
			item = &analyticsChannelRow{Code: row.Code, Name: row.Code}
			channelsByCode[row.Code] = item
			channelOrder = append(channelOrder, row.Code)
		}
		amount, parseErr := parseAnalyticsAmount(row.GrossRevenue)
		if parseErr != nil || amount < 0 {
			response.Error(c, 500, 50095, "error.channel_performance_amount_unsupported")
			return
		}
		item.PaidOrders, item.GrossRevenue = row.PaidOrders, amount
	}
	channels := make([]analyticsChannelRow, 0, len(channelOrder))
	for _, code := range channelOrder {
		channels = append(channels, *channelsByCode[code])
	}

	funnel := analyticsFunnel{Orders: metrics.OrdersCreated}
	funnelSQL := `SELECT COUNT(DISTINCT pi.order_id) FROM payment_intents pi JOIN orders o ON o.id = pi.order_id AND o.deleted_at IS NULL WHERE pi.deleted_at IS NULL AND o.created_at >= ? AND o.created_at < ? AND o.currency = ?`
	if err := h.DB.Raw(funnelSQL, window.From, window.To, currencyCode).Scan(&funnel.PaymentStarted).Error; err != nil ||
		h.DB.Model(&struct{ ID uuid.UUID }{}).Table("orders").Where("deleted_at IS NULL AND created_at >= ? AND created_at < ? AND currency = ? AND paid_at IS NOT NULL", window.From, window.To, currencyCode).Count(&funnel.Paid).Error != nil ||
		h.DB.Table("orders").Where("deleted_at IS NULL AND created_at >= ? AND created_at < ? AND currency = ? AND delivered_at IS NOT NULL", window.From, window.To, currencyCode).Count(&funnel.Delivered).Error != nil {
		response.Error(c, 500, 50095, "error.order_conversion_funnel_fetch_failed")
		return
	}
	funnel.PaymentRate = percentage(funnel.Paid, funnel.Orders)
	funnel.DeliveryRate = percentage(funnel.Delivered, funnel.Paid)

	response.OK(c, gin.H{
		"range":   gin.H{"from": window.From, "to": window.To, "granularity": window.Granularity, "timezone": "UTC", "currency": currencyCode},
		"metrics": metrics, "series": points, "products": products, "channels": channels, "funnel": funnel,
	})
}
