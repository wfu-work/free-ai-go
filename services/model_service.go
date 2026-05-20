package services

import (
	"encoding/json"
	"errors"
	"strings"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"gorm.io/gorm"
)

type ModelService struct{}

var ModelServiceApp = ModelService{}

type ModelInput struct {
	PublicModel   string `json:"publicModel"`
	Aliases       string `json:"aliases"`
	UpstreamModel string `json:"upstreamModel"`
	Provider      string `json:"provider"`
	AccountGroup  string `json:"accountGroup"`
	Stream        bool   `json:"stream"`
	TimeoutSec    int    `json:"timeoutSec"`
}

func (s ModelService) Create(input ModelInput) (domains.ModelMapping, error) {
	input.PublicModel = strings.TrimSpace(input.PublicModel)
	input.UpstreamModel = strings.TrimSpace(input.UpstreamModel)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Aliases = strings.TrimSpace(input.Aliases)
	if input.PublicModel == "" || input.UpstreamModel == "" {
		return domains.ModelMapping{}, errors.New("publicModel and upstreamModel are required")
	}
	input.AccountGroup = normalizeModelAccountGroup(input.AccountGroup)
	if input.TimeoutSec <= 0 {
		input.TimeoutSec = int(Config().RequestTimeoutSeconds)
	}
	entity := domains.ModelMapping{
		PublicModel:   input.PublicModel,
		Aliases:       input.Aliases,
		UpstreamModel: input.UpstreamModel,
		Provider:      input.Provider,
		AccountGroup:  input.AccountGroup,
		Stream:        input.Stream,
		TimeoutSec:    input.TimeoutSec,
		Enabled:       true,
	}
	err := global.NAV_DB.Create(&entity).Error
	if err == nil {
		AccountGroupServiceApp.RefreshSummaries(entity.AccountGroup)
	}
	AuditServiceApp.Record("", "model.create", "model", entity.Guid, map[string]string{"model": entity.PublicModel})
	return entity, err
}

func (s ModelService) Update(guid string, input ModelInput) (domains.ModelMapping, error) {
	var model domains.ModelMapping
	if err := global.NAV_DB.Where("guid = ?", guid).First(&model).Error; err != nil {
		return domains.ModelMapping{}, err
	}
	input.PublicModel = strings.TrimSpace(input.PublicModel)
	input.UpstreamModel = strings.TrimSpace(input.UpstreamModel)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Aliases = strings.TrimSpace(input.Aliases)
	if input.PublicModel == "" || input.UpstreamModel == "" {
		return domains.ModelMapping{}, errors.New("publicModel and upstreamModel are required")
	}
	input.AccountGroup = normalizeModelAccountGroup(input.AccountGroup)
	if input.TimeoutSec <= 0 {
		input.TimeoutSec = int(Config().RequestTimeoutSeconds)
	}
	if err := global.NAV_DB.Model(&model).Updates(map[string]any{
		"public_model":   input.PublicModel,
		"aliases":        input.Aliases,
		"upstream_model": input.UpstreamModel,
		"provider":       input.Provider,
		"account_group":  input.AccountGroup,
		"stream":         input.Stream,
		"timeout_sec":    input.TimeoutSec,
	}).Error; err != nil {
		return domains.ModelMapping{}, err
	}
	AccountGroupServiceApp.RefreshSummaries(model.AccountGroup, input.AccountGroup)
	AuditServiceApp.Record("", "model.update", "model", guid, map[string]string{"model": input.PublicModel})
	return s.Get(guid)
}

func (s ModelService) GetByGuid(guid string) (domains.ModelMapping, error) {
	var model domains.ModelMapping
	err := global.NAV_DB.Where("guid = ?", guid).First(&model).Error
	return model, err
}

func (s ModelService) Get(guid string) (domains.ModelMapping, error) {
	return s.GetByGuid(guid)
}

func (s ModelService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonUtils.Str2Int(params["size"])
	offset := limit * (commonUtils.Str2Int(params["page"]) - 1)
	var results []domains.ModelMapping
	db := global.NAV_DB.Model(new(domains.ModelMapping))
	if params["enabled"] != "" {
		db = db.Where("enabled = ?", params["enabled"])
	}
	if params["provider"] != "" {
		db = db.Where("provider = ?", params["provider"])
	}
	if params["accountGroup"] != "" {
		db = db.Where("account_group = ?", params["accountGroup"])
	}
	if params["content"] != "" {
		like := "%" + params["content"] + "%"
		db = db.Where("public_model LIKE ? OR aliases LIKE ? OR upstream_model LIKE ? OR provider LIKE ?", like, like, like, like)
	}
	if err = db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = db.Order("id desc").Limit(limit).Offset(offset).Find(&results).Error
	return results, total, err
}

func (s ModelService) ListAll() ([]domains.ModelMapping, error) {
	var list []domains.ModelMapping
	err := global.NAV_DB.Order("id desc").Find(&list).Error
	return list, err
}

func (s ModelService) ListEnabled() ([]domains.ModelMapping, error) {
	var list []domains.ModelMapping
	err := global.NAV_DB.Where("enabled = ?", true).Order("public_model asc").Find(&list).Error
	return list, err
}

func (s ModelService) Find(publicModel string) (domains.ModelMapping, error) {
	var model domains.ModelMapping
	err := global.NAV_DB.Where("public_model = ? AND enabled = ?", publicModel, true).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var candidates []domains.ModelMapping
		if listErr := global.NAV_DB.Where("enabled = ? AND aliases <> ?", true, "").Find(&candidates).Error; listErr != nil {
			return domains.ModelMapping{}, listErr
		}
		for _, candidate := range candidates {
			if modelAliasMatches(candidate.Aliases, publicModel) {
				return candidate, nil
			}
		}
		return domains.ModelMapping{}, errors.New(domains.ErrorModelNotSupported)
	}
	return model, err
}

func (s ModelService) SetEnabled(guid string, enabled bool) error {
	var model domains.ModelMapping
	_ = global.NAV_DB.Where("guid = ?", guid).First(&model).Error
	err := global.NAV_DB.Model(&domains.ModelMapping{}).Where("guid = ?", guid).Update("enabled", enabled).Error
	if err == nil && model.Guid != "" {
		AccountGroupServiceApp.RefreshSummaries(model.AccountGroup)
	}
	return err
}

func (s ModelService) PublicNames(model domains.ModelMapping) []string {
	names := []string{model.PublicModel}
	for _, alias := range parseAliases(model.Aliases) {
		if alias != "" && alias != model.PublicModel {
			names = append(names, alias)
		}
	}
	return names
}

func modelAliasMatches(raw, model string) bool {
	for _, alias := range parseAliases(raw) {
		if alias == model {
			return true
		}
	}
	return false
}

func parseAliases(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var aliases []string
	if err := json.Unmarshal([]byte(raw), &aliases); err == nil {
		return normalizeAliases(aliases)
	}
	return normalizeAliases(strings.Split(raw, ","))
}

func normalizeAliases(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func normalizeModelAccountGroup(value string) string {
	return strings.TrimSpace(value)
}

func (s ModelService) Delete(guid string) error {
	return s.DeleteByGuid(guid)
}

func (s ModelService) DeleteByGuid(guid string) error {
	var model domains.ModelMapping
	_ = global.NAV_DB.Where("guid = ?", guid).First(&model).Error
	err := global.NAV_DB.Where("guid = ?", guid).Delete(&domains.ModelMapping{}).Error
	if err == nil && model.Guid != "" {
		AccountGroupServiceApp.RefreshSummaries(model.AccountGroup)
	}
	AuditServiceApp.Record("", "model.delete", "model", guid, nil)
	return err
}
