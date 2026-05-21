package services

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"gorm.io/gorm"
)

type AccountGroupService struct{}

var AccountGroupServiceApp = AccountGroupService{}

type AccountGroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Sort        int    `json:"sort"`
	Enabled     *bool  `json:"enabled"`
	Remark      string `json:"remark"`
}

func (s AccountGroupService) Create(input AccountGroupInput) (domains.AccountGroup, error) {
	input.Name = normalizeAccountGroupName(input.Name)
	if input.Name == "" {
		return domains.AccountGroup{}, errors.New("name is required")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	var existing domains.AccountGroup
	err := global.NAV_DB.Unscoped().Where("name = ?", input.Name).First(&existing).Error
	if err == nil {
		if existing.DeletedTime.Valid {
			updates := map[string]any{
				"description":             strings.TrimSpace(input.Description),
				"sort":                    input.Sort,
				"enabled":                 enabled,
				"remark":                  input.Remark,
				"provider_summary":        "",
				"account_type_summary":    "",
				"model_summary":           "",
				"account_count":           0,
				"enabled_account_count":   0,
				"available_account_count": 0,
				"model_count":             0,
				"enabled_model_count":     0,
				"summary_synced_at":       int64(0),
				"deleted_time":            nil,
			}
			if err := global.NAV_DB.Unscoped().Model(&existing).Updates(updates).Error; err != nil {
				return domains.AccountGroup{}, err
			}
			_ = s.RefreshSummary(existing.Name)
			AuditServiceApp.Record("", "account_group.restore", "account_group", existing.Guid, map[string]string{"name": existing.Name})
			return s.GetByGuid(existing.Guid)
		}
		return domains.AccountGroup{}, errors.New("account group already exists")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return domains.AccountGroup{}, err
	}
	entity := domains.AccountGroup{
		Name:        input.Name,
		Description: strings.TrimSpace(input.Description),
		Sort:        input.Sort,
		Enabled:     enabled,
		Remark:      input.Remark,
	}
	err = global.NAV_DB.Create(&entity).Error
	if err == nil {
		_ = s.RefreshSummary(entity.Name)
	}
	AuditServiceApp.Record("", "account_group.create", "account_group", entity.Guid, map[string]string{"name": entity.Name})
	return entity, err
}

func (s AccountGroupService) Update(guid string, input AccountGroupInput) (domains.AccountGroup, error) {
	var entity domains.AccountGroup
	if err := global.NAV_DB.Where("guid = ?", guid).First(&entity).Error; err != nil {
		return domains.AccountGroup{}, err
	}
	name := normalizeAccountGroupName(input.Name)
	if name == "" {
		return domains.AccountGroup{}, errors.New("name is required")
	}
	enabled := entity.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	updates := map[string]any{
		"name":        name,
		"description": strings.TrimSpace(input.Description),
		"sort":        input.Sort,
		"enabled":     enabled,
		"remark":      input.Remark,
	}
	if err := global.NAV_DB.Model(&entity).Updates(updates).Error; err != nil {
		return domains.AccountGroup{}, err
	}
	if entity.Name != name {
		_ = s.RefreshSummary(entity.Name)
	}
	_ = s.RefreshSummary(name)
	AuditServiceApp.Record("", "account_group.update", "account_group", guid, map[string]string{"name": name})
	return s.Get(guid)
}

func (s AccountGroupService) GetByGuid(guid string) (domains.AccountGroup, error) {
	var entity domains.AccountGroup
	err := global.NAV_DB.Where("guid = ?", guid).First(&entity).Error
	return entity, err
}

func (s AccountGroupService) Get(guid string) (domains.AccountGroup, error) {
	return s.GetByGuid(guid)
}

func (s AccountGroupService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonUtils.Str2Int(params["size"])
	offset := limit * (commonUtils.Str2Int(params["page"]) - 1)
	var results []domains.AccountGroup
	db := global.NAV_DB.Model(new(domains.AccountGroup))
	if params["enabled"] != "" {
		db = db.Where("enabled = ?", params["enabled"])
	}
	if params["content"] != "" {
		like := "%" + params["content"] + "%"
		db = db.Where("name LIKE ? OR description LIKE ? OR remark LIKE ? OR provider_summary LIKE ? OR account_type_summary LIKE ? OR model_summary LIKE ?", like, like, like, like, like, like)
	}
	if err = db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = db.Order("sort asc, id asc").Limit(limit).Offset(offset).Find(&results).Error
	return results, total, err
}

func (s AccountGroupService) ListAll() ([]domains.AccountGroup, error) {
	var list []domains.AccountGroup
	err := global.NAV_DB.Order("sort asc, id asc").Find(&list).Error
	return list, err
}

func (s AccountGroupService) DeleteByGuid(guid string) error {
	var entity domains.AccountGroup
	if err := global.NAV_DB.Where("guid = ?", guid).First(&entity).Error; err != nil {
		return err
	}
	var usedAccounts int64
	_ = global.NAV_DB.Model(&domains.Account{}).Where("account_group = ?", entity.Name).Count(&usedAccounts).Error
	var usedModels int64
	_ = global.NAV_DB.Model(&domains.ModelMapping{}).Where("account_group = ?", entity.Name).Count(&usedModels).Error
	if usedAccounts+usedModels > 0 {
		return errors.New("account group is in use")
	}
	err := global.NAV_DB.Unscoped().Delete(&entity).Error
	AuditServiceApp.Record("", "account_group.delete", "account_group", guid, map[string]string{"name": entity.Name})
	return err
}

func (s AccountGroupService) Delete(guid string) error {
	return s.DeleteByGuid(guid)
}

func (s AccountGroupService) EnsureDefaults() error {
	groups := map[string]bool{"default": true}
	var accounts []domains.Account
	if err := global.NAV_DB.Select("account_group").Find(&accounts).Error; err == nil {
		for _, account := range accounts {
			if name := normalizeAccountGroupName(account.AccountGroup); name != "" {
				groups[name] = true
			}
		}
	}
	var models []domains.ModelMapping
	if err := global.NAV_DB.Select("account_group").Find(&models).Error; err == nil {
		for _, model := range models {
			if name := normalizeAccountGroupName(model.AccountGroup); name != "" {
				groups[name] = true
			}
		}
	}
	for name := range groups {
		var entity domains.AccountGroup
		err := global.NAV_DB.Where("name = ?", name).First(&entity).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if _, createErr := s.Create(AccountGroupInput{Name: name}); createErr != nil {
				return createErr
			}
			continue
		}
		if err != nil {
			return err
		}
	}
	for name := range groups {
		_ = s.RefreshSummary(name)
	}
	return nil
}

func (s AccountGroupService) RefreshSummary(groupName string) error {
	groupName = normalizeAccountGroupName(groupName)
	var entity domains.AccountGroup
	err := global.NAV_DB.Where("name = ?", groupName).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		entity = domains.AccountGroup{Name: groupName, Enabled: true}
		if err := global.NAV_DB.Create(&entity).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	var accounts []domains.Account
	accountQuery := global.NAV_DB.Where("account_group = ?", groupName)
	if groupName == "default" {
		accountQuery = global.NAV_DB.Where("account_group = ? OR account_group = ?", groupName, "")
	}
	if err := accountQuery.Find(&accounts).Error; err != nil {
		return err
	}
	var models []domains.ModelMapping
	modelQuery := global.NAV_DB.Where("account_group = ?", groupName)
	if groupName == "default" {
		modelQuery = global.NAV_DB.Where("account_group = ? OR account_group = ?", groupName, "")
	}
	if err := modelQuery.Find(&models).Error; err != nil {
		return err
	}

	providers := make([]string, 0)
	accountTypes := make([]string, 0)
	publicModels := make([]string, 0)
	enabledAccounts := 0
	availableAccounts := 0
	enabledModels := 0
	for _, account := range accounts {
		providers = append(providers, account.Provider)
		accountTypes = append(accountTypes, account.AccountType)
		if account.Enabled {
			enabledAccounts++
		}
		if account.Enabled && account.Status == domains.AccountStatusAvailable {
			availableAccounts++
		}
	}
	for _, model := range models {
		providers = append(providers, model.Provider)
		if model.PublicModel != "" {
			publicModels = append(publicModels, model.PublicModel)
		}
		if model.Enabled {
			enabledModels++
		}
	}

	updates := map[string]any{
		"provider_summary":        toJSONString(uniqueStrings(providers)),
		"account_type_summary":    toJSONString(uniqueStrings(accountTypes)),
		"model_summary":           toJSONString(uniqueStrings(publicModels)),
		"account_count":           len(accounts),
		"enabled_account_count":   enabledAccounts,
		"available_account_count": availableAccounts,
		"model_count":             len(models),
		"enabled_model_count":     enabledModels,
		"summary_synced_at":       time.Now().UnixMilli(),
	}
	return global.NAV_DB.Model(&entity).Updates(updates).Error
}

func (s AccountGroupService) RefreshSummaries(groupNames ...string) {
	seen := map[string]bool{}
	for _, groupName := range groupNames {
		normalized := normalizeAccountGroupName(groupName)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		_ = s.RefreshSummary(normalized)
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func toJSONString(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func normalizeAccountGroupName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return value
}
