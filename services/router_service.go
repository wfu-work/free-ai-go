package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type RouterService struct{}

var RouterServiceApp = RouterService{}

var routeStateLocks sync.Map

const (
	routingStrategyPriorityFirst            = "priority_first"
	routingStrategyRoundRobin               = "round_robin"
	routingStrategyAdaptiveWeighted         = "weighted_round_robin"
	routingStrategyStaticWeightedRoundRobin = "static_weighted_round_robin"
	routingStrategyLeastRecentlyUsed        = "least_recently_used"
	routingStrategyMostQuotaRemaining       = "most_quota_remaining"
	routingStrategySessionAffinity          = "session_affinity"
	routingStrategyQuotaAwareAdaptive       = "quota_aware_adaptive"
)

type RouteSelection struct {
	Model                 RoutedModel     `json:"model"`
	Account               domains.Account `json:"account"`
	AvailableAccountCount int             `json:"availableAccountCount"`
	AdaptiveAcquired      bool            `json:"-"`
}

func (s RouterService) Select(modelName string) (RouteSelection, error) {
	return s.SelectExcluding(modelName, nil)
}

func (s RouterService) SelectExcluding(modelName string, excluded map[string]bool) (RouteSelection, error) {
	return s.SelectForKey(modelName, excluded, domains.PlatformKey{})
}

// HasAvailableAccount 判断模型在平台密钥限定范围内是否至少存在一个可路由账号。
func (s RouterService) HasAvailableAccount(model RoutedModel, key domains.PlatformKey) bool {
	accountGroup := model.AccountGroup
	if key.AccountGroupFilter != "" {
		accountGroup = key.AccountGroupFilter
	}
	accounts, err := AccountServiceApp.FindAvailable(accountGroup, model.UpstreamModel, 100)
	if err != nil || len(accounts) == 0 {
		return false
	}
	availableGuids, err := ModelServiceApp.AvailableAccountGuids(model.CatalogGuid)
	if err != nil {
		return false
	}
	for _, account := range accounts {
		if availableGuids[account.Guid] {
			return true
		}
	}
	return false
}

func (s RouterService) SelectForKey(modelName string, excluded map[string]bool, key domains.PlatformKey) (RouteSelection, error) {
	return s.SelectForKeyWithAffinity(modelName, excluded, key, "")
}

func (s RouterService) SelectForKeyWithAffinity(modelName string, excluded map[string]bool, key domains.PlatformKey, affinityKey string) (RouteSelection, error) {
	model, err := ModelServiceApp.Find(modelName)
	if err != nil {
		return RouteSelection{}, err
	}
	accountGroup := model.AccountGroup
	if key.AccountGroupFilter != "" {
		accountGroup = key.AccountGroupFilter
	}
	accounts, err := AccountServiceApp.FindAvailable(accountGroup, model.UpstreamModel, 100)
	if err != nil {
		return RouteSelection{}, err
	}
	availableGuids, err := ModelServiceApp.AvailableAccountGuids(model.CatalogGuid)
	if err != nil {
		return RouteSelection{}, err
	}
	candidates := make([]domains.Account, 0, len(accounts))
	for _, account := range accounts {
		if excluded != nil && excluded[account.Guid] {
			continue
		}
		if availableGuids[account.Guid] {
			candidates = append(candidates, account)
		}
	}
	if len(candidates) == 0 {
		return RouteSelection{}, errors.New(domains.ErrorNoAvailableAccount)
	}
	strategy := Config().RoutingStrategy
	if strings.TrimSpace(affinityKey) == "" {
		affinityKey = key.Guid
	}
	account := s.pickByStrategyWithAffinity(model, candidates, strategy, affinityKey)
	return RouteSelection{
		Model:                 model,
		Account:               account,
		AvailableAccountCount: len(candidates),
		AdaptiveAcquired:      strategy == routingStrategyAdaptiveWeighted || strategy == routingStrategyQuotaAwareAdaptive,
	}, nil
}

func (s RouterService) pick(model RoutedModel, accounts []domains.Account) domains.Account {
	strategy := Config().RoutingStrategy
	return s.pickByStrategy(model, accounts, strategy)
}

func (s RouterService) pickByStrategy(model RoutedModel, accounts []domains.Account, strategy string) domains.Account {
	return s.pickByStrategyWithAffinity(model, accounts, strategy, "")
}

func (s RouterService) pickByStrategyWithAffinity(model RoutedModel, accounts []domains.Account, strategy, affinityKey string) domains.Account {
	switch strategy {
	case routingStrategyRoundRobin:
		return s.pickRoundRobin(model, accounts, false)
	case routingStrategyAdaptiveWeighted:
		return s.pickAdaptiveWeighted(model, accounts, false)
	case routingStrategyQuotaAwareAdaptive:
		return s.pickAdaptiveWeighted(model, accounts, true)
	case routingStrategyStaticWeightedRoundRobin:
		return s.pickRoundRobin(model, accounts, true)
	case routingStrategyLeastRecentlyUsed:
		sort.SliceStable(accounts, func(i, j int) bool {
			return accounts[i].LastUsedAt < accounts[j].LastUsedAt
		})
		return accounts[0]
	case routingStrategyMostQuotaRemaining:
		if account, ok := s.pickMostQuotaRemaining(accounts); ok {
			return account
		}
		return accounts[0]
	case routingStrategySessionAffinity:
		return pickSessionAffinityAccount(model, accounts, affinityKey)
	case routingStrategyPriorityFirst:
		fallthrough
	default:
		return accounts[0]
	}
}

func (s RouterService) pickAdaptiveWeighted(model RoutedModel, accounts []domains.Account, quotaAware bool) domains.Account {
	mode := "adaptive"
	quotaFactors := map[string]float64(nil)
	if quotaAware {
		mode = "quota-adaptive"
		quotaFactors, _ = loadQuotaAdaptiveRouteFactors(accounts, time.Now())
	}
	routeKey := fmt.Sprintf("%s:%s:%s", model.AccountGroup, model.PublicModel, mode)
	state := domains.RouteState{}
	cursor, redisCursor := s.redisRouteCursor(routeKey)
	if !redisCursor {
		lockValue, _ := routeStateLocks.LoadOrStore(routeKey, &sync.Mutex{})
		lock := lockValue.(*sync.Mutex)
		lock.Lock()
		defer lock.Unlock()
		state = s.routeState(routeKey)
		cursor = state.Cursor
	}
	selected := adaptiveRouteMetrics.pickAndAcquireWithQuota(accounts, cursor, time.Now(), quotaFactors)
	if selected.Guid == "" {
		selected = accounts[0]
	}
	if !redisCursor {
		s.saveRouteState(state, selected.Guid, cursor+1)
	}
	return selected
}

// ObserveAttempt releases the account's in-flight slot and feeds the latest
// first-response and overload outcome back into adaptive routing.
func (s RouterService) ObserveAttempt(selection RouteSelection, result *ProxyResult, err error) {
	adaptiveRouteMetrics.observe(selection.Account.Guid, selection.AdaptiveAcquired, result, err, time.Now())
}

func (s RouterService) pickRoundRobin(model RoutedModel, accounts []domains.Account, weighted bool) domains.Account {
	routeKey := fmt.Sprintf("%s:%s:%t", model.AccountGroup, model.PublicModel, weighted)
	state := domains.RouteState{}
	cursor, redisCursor := s.redisRouteCursor(routeKey)
	if !redisCursor {
		lockValue, _ := routeStateLocks.LoadOrStore(routeKey, &sync.Mutex{})
		lock := lockValue.(*sync.Mutex)
		lock.Lock()
		defer lock.Unlock()
		state = s.routeState(routeKey)
		cursor = state.Cursor
	}
	selected := pickRoundRobinAccount(accounts, cursor, weighted)
	if !redisCursor {
		s.saveRouteState(state, selected.Guid, cursor+1)
	}
	return selected
}

func pickRoundRobinAccount(accounts []domains.Account, cursor int, weighted bool) domains.Account {
	if len(accounts) == 0 {
		return domains.Account{}
	}
	cursor = max(cursor, 0)
	if weighted {
		totalWeight := 0
		for _, account := range accounts {
			totalWeight += normalizedRouteWeight(account.Weight)
		}
		offset := cursor % totalWeight
		for _, account := range accounts {
			weight := normalizedRouteWeight(account.Weight)
			if offset < weight {
				return account
			}
			offset -= weight
		}
	}
	return accounts[cursor%len(accounts)]
}

func normalizedRouteWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return min(weight, 1000)
}

// redisRouteCursor 在启用 Redis 时使用原子自增游标，使多实例共享同一轮询顺序。
func (s RouterService) redisRouteCursor(routeKey string) (int, bool) {
	if global.NAV_REDIS == nil {
		return 0, false
	}
	value, err := global.NAV_REDIS.Incr(context.Background(), "freeai:route:cursor:"+routeKey).Result()
	if err != nil || value <= 0 {
		return 0, false
	}
	return int(value - 1), true
}

func (s RouterService) routeState(routeKey string) domains.RouteState {
	var state domains.RouteState
	err := global.NAV_DB.Where("route_key = ?", routeKey).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		state = domains.RouteState{RouteKey: routeKey}
	}
	return state
}

func (s RouterService) saveRouteState(state domains.RouteState, accountGuid string, cursor int) {
	state.LastAccountGuid = accountGuid
	state.Cursor = cursor
	state.UpdatedAtUnix = time.Now().UnixMilli()
	if state.Guid == "" {
		_ = global.NAV_DB.Create(&state).Error
		return
	}
	_ = global.NAV_DB.Model(&state).Updates(map[string]any{
		"last_account_guid": accountGuid,
		"cursor":            cursor,
		"updated_at_unix":   state.UpdatedAtUnix,
	}).Error
}

func (s RouterService) pickMostQuotaRemaining(accounts []domains.Account) (domains.Account, bool) {
	guids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		guids = append(guids, account.Guid)
	}
	var quotas []domains.AccountQuota
	if err := global.NAV_DB.Where("account_guid IN ?", guids).Find(&quotas).Error; err != nil {
		return domains.Account{}, false
	}
	type quotaScore struct {
		known bool
		used  float64
		reset int64
	}
	scores := map[string]quotaScore{}
	now := time.Now().UnixMilli()
	for _, quota := range quotas {
		if quota.ResetAt > 0 && quota.ResetAt <= now || quota.UsedPercent == nil {
			continue
		}
		score := scores[quota.AccountGuid]
		if !score.known || *quota.UsedPercent > score.used {
			score.known = true
			score.used = *quota.UsedPercent
		}
		if quota.ResetAt > score.reset {
			score.reset = quota.ResetAt
		}
		scores[quota.AccountGuid] = score
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		left, right := scores[accounts[i].Guid], scores[accounts[j].Guid]
		if left.known != right.known {
			return left.known
		}
		if left.used != right.used {
			return left.used < right.used
		}
		if left.reset != right.reset {
			return left.reset < right.reset
		}
		return accounts[i].LastUsedAt < accounts[j].LastUsedAt
	})
	return accounts[0], true
}
