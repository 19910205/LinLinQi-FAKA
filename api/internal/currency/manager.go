package currency

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

type Manager struct {
	DB           *gorm.DB
	AllowPrivate bool
	Now          func() time.Time
}

type liveCandidate struct {
	config model.FXProviderConfig
	quote  ProviderQuote
	rate   *big.Rat
	obs    model.FXRateObservation
}

type liveFetchResult struct {
	config model.FXProviderConfig
	quote  ProviderQuote
	err    error
}

// prioritizeProviderDrivers gives every independent transport/driver an early
// slot before additional feeds from the same aggregator. Otherwise dozens of
// Frankfurter provider filters can occupy the whole semaphore and starve the
// independent open exchange-rate source until the global deadline.
func prioritizeProviderDrivers(configs []model.FXProviderConfig) []model.FXProviderConfig {
	first, remaining := make([]model.FXProviderConfig, 0, len(configs)), make([]model.FXProviderConfig, 0, len(configs))
	seen := make(map[string]bool)
	for _, config := range configs {
		driver := strings.ToLower(strings.TrimSpace(config.Driver))
		if !seen[driver] {
			seen[driver] = true
			first = append(first, config)
		} else {
			remaining = append(remaining, config)
		}
	}
	return append(first, remaining...)
}

type Conversion struct {
	SourceAmount     int64
	SourceCurrency   string
	TargetAmount     int64
	TargetCurrency   string
	ConvertedCost    int64
	MarkupBasisPoint int
	Rate             string
	Snapshot         model.FXRateSnapshot
}

func (manager Manager) now() time.Time {
	if manager.Now != nil {
		return manager.Now().UTC()
	}
	return time.Now().UTC()
}

func exactDecimal(rate *big.Rat) string {
	value := rate.FloatString(18)
	value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	if value == "" {
		return "0"
	}
	return value
}

func earlierTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func rateDeviationWithin(value, median *big.Rat, basisPoints int64) bool {
	difference := new(big.Rat).Sub(value, median)
	if difference.Sign() < 0 {
		difference.Neg(difference)
	}
	allowed := new(big.Rat).Mul(median, new(big.Rat).SetFrac(big.NewInt(basisPoints), big.NewInt(10_000)))
	return difference.Cmp(allowed) <= 0
}

func (manager Manager) recordProviderFailure(config model.FXProviderConfig, now time.Time, err error) {
	_ = manager.DB.Model(&model.FXProviderConfig{}).Where("id = ?", config.ID).Updates(map[string]any{
		"failure_count": gorm.Expr("failure_count + 1"), "last_failure_at": &now,
		"last_error": supplierSafeError(err),
	}).Error
}

func supplierSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

func (manager Manager) live(ctx context.Context, baseCode, quoteCode string, now time.Time) (*model.FXRateSnapshot, error) {
	var configs []model.FXProviderConfig
	if err := manager.DB.Where("enabled = ?", true).Order("priority ASC, code ASC").Limit(32).Find(&configs).Error; err != nil {
		return nil, err
	}
	if len(configs) == 0 {
		return nil, ErrRateUnavailable
	}
	configs = prioritizeProviderDrivers(configs)
	resolveCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	results := make(chan liveFetchResult, len(configs))
	semaphore := make(chan struct{}, 5)
	var wait sync.WaitGroup
	for _, config := range configs {
		config := config
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-resolveCtx.Done():
				results <- liveFetchResult{config: config, err: resolveCtx.Err()}
				return
			}
			provider, err := NewProvider(config, manager.AllowPrivate)
			if err != nil {
				results <- liveFetchResult{config: config, err: err}
				return
			}
			quote, err := provider.Quote(resolveCtx, baseCode, quoteCode)
			results <- liveFetchResult{config: config, quote: quote, err: err}
		}()
	}
	go func() {
		wait.Wait()
		close(results)
	}()
	candidates := make([]liveCandidate, 0, len(configs))
	for result := range results {
		config, quote, err := result.config, result.quote, result.err
		if err != nil {
			manager.recordProviderFailure(config, now, err)
			continue
		}
		rate, err := ParseRate(quote.Rate)
		if err != nil || quote.ObservedAt.Before(now.Add(-96*time.Hour)) || quote.ObservedAt.After(now.Add(15*time.Minute)) {
			if err == nil {
				err = errors.New("exchange-rate observation is outside the freshness window")
			}
			manager.recordProviderFailure(config, now, err)
			observation := model.FXRateObservation{ProviderID: config.ID, BaseCode: baseCode, QuoteCode: quoteCode, Rate: quote.Rate, ObservedAt: quote.ObservedAt, FetchedAt: now, RawHash: quote.RawHash, Accepted: false, RejectCode: "stale_or_invalid"}
			_ = manager.DB.Create(&observation).Error
			continue
		}
		observation := model.FXRateObservation{ProviderID: config.ID, BaseCode: baseCode, QuoteCode: quoteCode, Rate: quote.Rate, ObservedAt: quote.ObservedAt, FetchedAt: now, RawHash: quote.RawHash, Accepted: false}
		if err := manager.DB.Create(&observation).Error; err != nil {
			return nil, err
		}
		_ = manager.DB.Model(&model.FXProviderConfig{}).Where("id = ?", config.ID).Updates(map[string]any{"failure_count": 0, "last_success_at": &now, "last_error": ""}).Error
		candidates = append(candidates, liveCandidate{config: config, quote: quote, rate: rate, obs: observation})
	}
	if len(candidates) < 2 {
		return nil, ErrRateUnavailable
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].rate.Cmp(candidates[right].rate) < 0 })
	median := candidates[len(candidates)/2].rate
	accepted := make([]liveCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if rateDeviationWithin(candidate.rate, median, 500) {
			accepted = append(accepted, candidate)
			_ = manager.DB.Model(&model.FXRateObservation{}).Where("id = ?", candidate.obs.ID).Updates(map[string]any{"accepted": true, "reject_code": ""}).Error
		} else {
			_ = manager.DB.Model(&model.FXRateObservation{}).Where("id = ?", candidate.obs.ID).Update("reject_code", "outlier").Error
		}
	}
	if len(accepted) < 2 {
		return nil, ErrRateUnavailable
	}
	sort.Slice(accepted, func(left, right int) bool { return accepted[left].rate.Cmp(accepted[right].rate) < 0 })
	selected := accepted[len(accepted)/2]
	observedAt := selected.quote.ObservedAt
	for _, candidate := range accepted {
		if candidate.quote.ObservedAt.After(observedAt) {
			observedAt = candidate.quote.ObservedAt
		}
	}
	snapshot := model.FXRateSnapshot{BaseCode: baseCode, QuoteCode: quoteCode, Rate: selected.quote.Rate, SourceTier: "live", ProviderID: &selected.config.ID, ObservedAt: observedAt, SelectedAt: now, ExpiresAt: now.Add(6 * time.Hour), StaleAfter: now.Add(7 * 24 * time.Hour), ConsensusCount: len(accepted), Decision: fmt.Sprintf("median consensus from %d accepted live providers", len(accepted))}
	if err := manager.DB.Create(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (manager Manager) manual(baseCode, quoteCode string, now time.Time) (*model.FXRateSnapshot, error) {
	var rate model.FXManualRate
	active := func() *gorm.DB {
		return manager.DB.Where("enabled = ? AND valid_from <= ? AND (valid_to IS NULL OR valid_to > ?)", true, now, now)
	}
	if err := active().Where("base_code = ? AND quote_code = ?", baseCode, quoteCode).Order("valid_from DESC").First(&rate).Error; err == nil {
		expiresAt := now.Add(24 * time.Hour)
		if rate.ValidTo != nil && rate.ValidTo.Before(expiresAt) {
			expiresAt = *rate.ValidTo
		}
		snapshot := model.FXRateSnapshot{BaseCode: baseCode, QuoteCode: quoteCode, Rate: rate.Rate, SourceTier: "manual", ManualRateID: &rate.ID, ObservedAt: rate.ValidFrom, SelectedAt: now, ExpiresAt: expiresAt, StaleAfter: expiresAt, ConsensusCount: 1, Decision: "live providers unavailable; selected active manual rate"}
		if err := manager.DB.Create(&snapshot).Error; err != nil {
			return nil, err
		}
		return &snapshot, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	rate = model.FXManualRate{}
	if err := active().Where("base_code = ? AND quote_code = ?", quoteCode, baseCode).Order("valid_from DESC").First(&rate).Error; err == nil {
		parsed, parseErr := ParseRate(rate.Rate)
		if parseErr != nil {
			return nil, parseErr
		}
		inverse := new(big.Rat).Inv(parsed)
		expiresAt := now.Add(24 * time.Hour)
		if rate.ValidTo != nil && rate.ValidTo.Before(expiresAt) {
			expiresAt = *rate.ValidTo
		}
		snapshot := model.FXRateSnapshot{BaseCode: baseCode, QuoteCode: quoteCode, Rate: exactDecimal(inverse), SourceTier: "manual", ManualRateID: &rate.ID, ObservedAt: rate.ValidFrom, SelectedAt: now, ExpiresAt: expiresAt, StaleAfter: expiresAt, ConsensusCount: 1, Decision: "live providers unavailable; selected inverse active manual rate"}
		if err := manager.DB.Create(&snapshot).Error; err != nil {
			return nil, err
		}
		return &snapshot, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return nil, ErrRateUnavailable
}

func (manager Manager) cached(baseCode, quoteCode string, now time.Time) (*model.FXRateSnapshot, error) {
	var parent model.FXRateSnapshot
	if err := manager.DB.Where("base_code = ? AND quote_code = ? AND source_tier IN ? AND stale_after > ?", baseCode, quoteCode, []string{"live", "manual"}, now).Order("selected_at DESC").First(&parent).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err := manager.DB.Where("base_code = ? AND quote_code = ? AND source_tier IN ? AND stale_after > ?", quoteCode, baseCode, []string{"live", "manual"}, now).Order("selected_at DESC").First(&parent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrRateUnavailable
			}
			return nil, err
		}
		parsed, err := ParseRate(parent.Rate)
		if err != nil {
			return nil, err
		}
		inverse := exactDecimal(new(big.Rat).Inv(parsed))
		snapshot := model.FXRateSnapshot{BaseCode: baseCode, QuoteCode: quoteCode, Rate: inverse, SourceTier: "cached", ParentSnapshotID: &parent.ID, ObservedAt: parent.ObservedAt, SelectedAt: now, ExpiresAt: earlierTime(now.Add(15*time.Minute), parent.StaleAfter), StaleAfter: parent.StaleAfter, ConsensusCount: parent.ConsensusCount, Decision: "live and manual rates unavailable; reused inverse of latest trusted cached snapshot"}
		if err := manager.DB.Create(&snapshot).Error; err != nil {
			return nil, err
		}
		return &snapshot, nil
	}
	snapshot := model.FXRateSnapshot{BaseCode: baseCode, QuoteCode: quoteCode, Rate: parent.Rate, SourceTier: "cached", ParentSnapshotID: &parent.ID, ObservedAt: parent.ObservedAt, SelectedAt: now, ExpiresAt: earlierTime(now.Add(15*time.Minute), parent.StaleAfter), StaleAfter: parent.StaleAfter, ConsensusCount: parent.ConsensusCount, Decision: "live and manual rates unavailable; reused latest trusted cached snapshot"}
	if err := manager.DB.Create(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (manager Manager) Resolve(ctx context.Context, baseCode, quoteCode string) (model.FXRateSnapshot, error) {
	baseCode, quoteCode, err := normalizePair(baseCode, quoteCode)
	if err != nil {
		return model.FXRateSnapshot{}, err
	}
	now := manager.now()
	if baseCode == quoteCode {
		snapshot := model.FXRateSnapshot{BaseCode: baseCode, QuoteCode: quoteCode, Rate: "1", SourceTier: "system", ObservedAt: now, SelectedAt: now, ExpiresAt: now.Add(365 * 24 * time.Hour), StaleAfter: now.Add(365 * 24 * time.Hour), ConsensusCount: 1, Decision: "same-currency conversion"}
		if err := manager.DB.Create(&snapshot).Error; err != nil {
			return model.FXRateSnapshot{}, err
		}
		return snapshot, nil
	}
	if snapshot, err := manager.live(ctx, baseCode, quoteCode, now); err == nil {
		return *snapshot, nil
	}
	if snapshot, err := manager.manual(baseCode, quoteCode, now); err == nil {
		return *snapshot, nil
	}
	if snapshot, err := manager.cached(baseCode, quoteCode, now); err == nil {
		return *snapshot, nil
	}
	return model.FXRateSnapshot{}, ErrRateUnavailable
}

func (manager Manager) currency(code string) (model.CurrencyDefinition, error) {
	var currency model.CurrencyDefinition
	err := manager.DB.Where("code = ? AND enabled = ?", strings.ToUpper(strings.TrimSpace(code)), true).First(&currency).Error
	return currency, err
}

func (manager Manager) ConvertWithMarkup(ctx context.Context, amount int64, sourceCode, targetCode string, markupBasisPoint int) (Conversion, error) {
	source, err := manager.currency(sourceCode)
	if err != nil {
		return Conversion{}, err
	}
	target, err := manager.currency(targetCode)
	if err != nil {
		return Conversion{}, err
	}
	snapshot, err := manager.Resolve(ctx, source.Code, target.Code)
	if err != nil {
		return Conversion{}, err
	}
	cost, err := Convert(amount, source.MinorUnit, target.MinorUnit, snapshot.Rate)
	if err != nil {
		return Conversion{}, err
	}
	sale, err := ConvertWithMarkup(amount, source.MinorUnit, target.MinorUnit, snapshot.Rate, markupBasisPoint)
	if err != nil {
		return Conversion{}, err
	}
	return Conversion{SourceAmount: amount, SourceCurrency: source.Code, TargetAmount: sale, TargetCurrency: target.Code, ConvertedCost: cost, MarkupBasisPoint: markupBasisPoint, Rate: snapshot.Rate, Snapshot: snapshot}, nil
}
