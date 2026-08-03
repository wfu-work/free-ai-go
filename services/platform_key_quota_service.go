package services

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PlatformKeyQuotaReservation 表示一次已经成功写入数据库的 Token 额度预占。
type PlatformKeyQuotaReservation struct {
	RequestID string
	Guid      string
	Tokens    int64
}

// ReserveTokens 在真正调用上游前预占平台密钥额度。
//
// 该操作会锁定平台密钥记录，并把其它未过期请求的预占量一起纳入判断，避免并发请求同时越过总额度。
func (s PlatformKeyService) ReserveTokens(key domains.PlatformKey, requestID string, body []byte) (PlatformKeyQuotaReservation, error) {
	limit := s.EffectiveTokenLimit(key)
	if limit <= 0 {
		return PlatformKeyQuotaReservation{RequestID: requestID}, nil
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = uuid.NewString()
	}
	reservedTokens := estimateQuotaReservationTokens(body)
	reservation := PlatformKeyQuotaReservation{RequestID: requestID, Tokens: reservedTokens}
	now := time.Now().UnixMilli()
	ttl := Config().QuotaReservationTTLSeconds
	if ttl <= 0 {
		ttl = 1800
	}
	err := global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		var current domains.PlatformKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("guid = ? AND enabled = ?", key.Guid, true).First(&current).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("platform_key_id = ? AND expires_at <= ?", current.Guid, now).Delete(&domains.PlatformKeyReservation{}).Error; err != nil {
			return err
		}
		var inFlight int64
		if err := tx.Model(&domains.PlatformKeyReservation{}).
			Where("platform_key_id = ? AND expires_at > ?", current.Guid, now).
			Select("COALESCE(SUM(reserved_tokens), 0)").Scan(&inFlight).Error; err != nil {
			return err
		}
		if current.UsedTokens >= limit || reservedTokens > limit-current.UsedTokens-inFlight {
			return errors.New(domains.ErrorPlatformKeyLimited)
		}
		row := domains.PlatformKeyReservation{
			RequestID: requestID, PlatformKeyID: current.Guid, ReservedTokens: reservedTokens,
			ExpiresAt: now + ttl*1000,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		reservation.Guid = row.Guid
		return nil
	})
	return reservation, err
}

// FinalizeTokens 释放预占，并把上游返回的真实 Token 和官方参考成本原子累计到平台密钥。
func (s PlatformKeyService) FinalizeTokens(keyGuid string, reservation PlatformKeyQuotaReservation, usedTokens, costMicrousd int64) error {
	if usedTokens < 0 {
		usedTokens = 0
	}
	if costMicrousd < 0 {
		costMicrousd = 0
	}
	return global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		if reservation.Guid != "" {
			if err := tx.Unscoped().Where("guid = ? AND platform_key_id = ?", reservation.Guid, keyGuid).
				Delete(&domains.PlatformKeyReservation{}).Error; err != nil {
				return err
			}
		}
		if usedTokens == 0 && costMicrousd == 0 {
			return nil
		}
		return tx.Model(&domains.PlatformKey{}).Where("guid = ?", keyGuid).Updates(map[string]any{
			"used_tokens":        gorm.Expr("used_tokens + ?", usedTokens),
			"used_cost_microusd": gorm.Expr("used_cost_microusd + ?", costMicrousd),
		}).Error
	})
}

// ReconcileUsageFromLogs 用历史请求日志补齐升级前的平台密钥累计用量。
// 该方法只向上修正，不会因为日志清理而把长期累计值调小。
func (s PlatformKeyService) ReconcileUsageFromLogs() error {
	type usageRow struct {
		PlatformKeyID string
		UsedTokens    int64
		CostMicrousd  int64
	}
	var rows []usageRow
	if err := global.NAV_DB.Model(&domains.RequestLog{}).
		Select("platform_key_id, COALESCE(SUM(input_tokens + output_tokens), 0) AS used_tokens, COALESCE(SUM(cost_microusd), 0) AS cost_microusd").
		Where("platform_key_id <> ?", "").Group("platform_key_id").Scan(&rows).Error; err != nil {
		return err
	}
	return global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			var key domains.PlatformKey
			if err := tx.Where("guid = ?", row.PlatformKeyID).First(&key).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			updates := map[string]any{}
			if row.UsedTokens > key.UsedTokens {
				updates["used_tokens"] = row.UsedTokens
			}
			if row.CostMicrousd > key.UsedCostMicrousd {
				updates["used_cost_microusd"] = row.CostMicrousd
			}
			if len(updates) > 0 {
				if err := tx.Model(&key).Updates(updates).Error; err != nil {
					return err
				}
			}
		}
		return tx.Unscoped().Where("expires_at <= ?", time.Now().UnixMilli()).Delete(&domains.PlatformKeyReservation{}).Error
	})
}

// estimateQuotaReservationTokens 使用请求大小估算输入 Token，并叠加客户端声明的最大输出量。
// 按每 3 字节一个 Token 估算会略偏保守，可减少真实用量超过预占量的概率。
func estimateQuotaReservationTokens(body []byte) int64 {
	inputEstimate := int64((len(body)+2)/3) + 256
	outputEstimate := Config().QuotaDefaultReserveTokens
	if outputEstimate <= 0 {
		outputEstimate = 8192
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) == nil {
		for _, field := range []string{"max_output_tokens", "max_completion_tokens", "max_tokens"} {
			if value, ok := positiveJSONInt(payload[field]); ok {
				outputEstimate = value
				break
			}
		}
	}
	if inputEstimate > math.MaxInt64-outputEstimate {
		return math.MaxInt64
	}
	return inputEstimate + outputEstimate
}

// positiveJSONInt 将 JSON 解码后的正整数统一转换为 int64。
func positiveJSONInt(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed > 0 && typed <= math.MaxInt64 {
			return int64(typed), true
		}
	case json.Number:
		number, err := typed.Int64()
		return number, err == nil && number > 0
	case int:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	}
	return 0, false
}
