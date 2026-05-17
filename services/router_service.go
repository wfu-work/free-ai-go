package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type RouterService struct{}

var RouterServiceApp = RouterService{}

type RouteSelection struct {
	Model   domains.ModelMapping `json:"model"`
	Account domains.Account      `json:"account"`
}

func (s RouterService) Select(modelName string) (RouteSelection, error) {
	return s.SelectExcluding(modelName, nil)
}

func (s RouterService) SelectExcluding(modelName string, excluded map[string]bool) (RouteSelection, error) {
	model, err := ModelServiceApp.Find(modelName)
	if err != nil {
		return RouteSelection{}, err
	}
	accounts, err := AccountServiceApp.FindAvailable(model.Provider, model.AccountGroup, model.UpstreamModel, 100)
	if err != nil {
		return RouteSelection{}, err
	}
	candidates := make([]domains.Account, 0, len(accounts))
	for _, account := range accounts {
		if excluded != nil && excluded[account.Guid] {
			continue
		}
		if supportsModel(account.SupportedModels, model.UpstreamModel) || supportsModel(account.SupportedModels, model.PublicModel) {
			candidates = append(candidates, account)
		}
	}
	if len(candidates) == 0 {
		for _, account := range accounts {
			if excluded != nil && excluded[account.Guid] {
				continue
			}
			candidates = append(candidates, account)
		}
	}
	if len(candidates) == 0 {
		return RouteSelection{}, errors.New(domains.ErrorNoAvailableAccount)
	}
	account := s.pick(model, candidates)
	return RouteSelection{Model: model, Account: account}, nil
}

func supportsModel(raw, model string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return true
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err == nil {
		for _, item := range models {
			if strings.TrimSpace(item) == model || strings.TrimSpace(item) == "*" {
				return true
			}
		}
		return false
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == model || part == "*" {
			return true
		}
	}
	return false
}

func (s RouterService) pick(model domains.ModelMapping, accounts []domains.Account) domains.Account {
	strategy := Config().RoutingStrategy
	switch strategy {
	case "round_robin":
		return s.pickRoundRobin(model, accounts, false)
	case "weighted_round_robin":
		return s.pickRoundRobin(model, accounts, true)
	case "least_recently_used":
		sort.SliceStable(accounts, func(i, j int) bool {
			return accounts[i].LastUsedAt < accounts[j].LastUsedAt
		})
		return accounts[0]
	case "most_quota_remaining":
		if account, ok := s.pickMostQuotaRemaining(accounts); ok {
			return account
		}
		return accounts[0]
	case "priority_first":
		fallthrough
	default:
		return accounts[0]
	}
}

func (s RouterService) pickRoundRobin(model domains.ModelMapping, accounts []domains.Account, weighted bool) domains.Account {
	routeKey := fmt.Sprintf("%s:%s:%s:%t", model.Provider, model.AccountGroup, model.PublicModel, weighted)
	state := s.routeState(routeKey)
	cursor := state.Cursor
	var selected domains.Account
	if weighted {
		totalWeight := 0
		for _, account := range accounts {
			if account.Weight <= 0 {
				account.Weight = 1
			}
			totalWeight += account.Weight
		}
		if totalWeight <= 0 {
			totalWeight = len(accounts)
		}
		offset := cursor % totalWeight
		for _, account := range accounts {
			weight := account.Weight
			if weight <= 0 {
				weight = 1
			}
			if offset < weight {
				selected = account
				break
			}
			offset -= weight
		}
	} else {
		selected = accounts[cursor%len(accounts)]
	}
	if selected.Guid == "" {
		selected = accounts[0]
	}
	s.saveRouteState(state, selected.Guid, cursor+1)
	return selected
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
	byGuid := map[string]domains.Account{}
	guids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		byGuid[account.Guid] = account
		guids = append(guids, account.Guid)
	}
	var quotas []domains.AccountQuota
	if err := global.NAV_DB.Where("account_guid IN ?", guids).Order("remaining_tokens desc").Find(&quotas).Error; err != nil {
		return domains.Account{}, false
	}
	for _, quota := range quotas {
		if account, ok := byGuid[quota.AccountGuid]; ok && quota.Status != domains.QuotaStatusExhausted {
			return account, true
		}
	}
	return domains.Account{}, false
}
