package services

import (
	"errors"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type ModelService struct{}

var ModelServiceApp = ModelService{}

type ModelInput struct {
	PublicModel   string `json:"publicModel"`
	UpstreamModel string `json:"upstreamModel"`
	Provider      string `json:"provider"`
	AccountGroup  string `json:"accountGroup"`
	Stream        bool   `json:"stream"`
	TimeoutSec    int    `json:"timeoutSec"`
}

func (s ModelService) Create(input ModelInput) (domains.ModelMapping, error) {
	if input.PublicModel == "" || input.UpstreamModel == "" || input.Provider == "" {
		return domains.ModelMapping{}, errors.New("publicModel, upstreamModel and provider are required")
	}
	if input.TimeoutSec <= 0 {
		input.TimeoutSec = int(Config().RequestTimeoutSeconds)
	}
	entity := domains.ModelMapping{
		PublicModel:   input.PublicModel,
		UpstreamModel: input.UpstreamModel,
		Provider:      input.Provider,
		AccountGroup:  input.AccountGroup,
		Stream:        input.Stream,
		TimeoutSec:    input.TimeoutSec,
		Enabled:       true,
	}
	err := global.NAV_DB.Create(&entity).Error
	AuditServiceApp.Record("", "model.create", "model", entity.Guid, map[string]string{"model": entity.PublicModel})
	return entity, err
}

func (s ModelService) Update(guid string, input ModelInput) (domains.ModelMapping, error) {
	var model domains.ModelMapping
	if err := global.NAV_DB.Where("guid = ?", guid).First(&model).Error; err != nil {
		return domains.ModelMapping{}, err
	}
	if input.PublicModel == "" || input.UpstreamModel == "" || input.Provider == "" {
		return domains.ModelMapping{}, errors.New("publicModel, upstreamModel and provider are required")
	}
	if input.TimeoutSec <= 0 {
		input.TimeoutSec = int(Config().RequestTimeoutSeconds)
	}
	if err := global.NAV_DB.Model(&model).Updates(map[string]any{
		"public_model":   input.PublicModel,
		"upstream_model": input.UpstreamModel,
		"provider":       input.Provider,
		"account_group":  input.AccountGroup,
		"stream":         input.Stream,
		"timeout_sec":    input.TimeoutSec,
	}).Error; err != nil {
		return domains.ModelMapping{}, err
	}
	AuditServiceApp.Record("", "model.update", "model", guid, map[string]string{"model": input.PublicModel})
	return s.Get(guid)
}

func (s ModelService) Get(guid string) (domains.ModelMapping, error) {
	var model domains.ModelMapping
	err := global.NAV_DB.Where("guid = ?", guid).First(&model).Error
	return model, err
}

func (s ModelService) List(limit int) ([]domains.ModelMapping, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var list []domains.ModelMapping
	err := global.NAV_DB.Order("id desc").Limit(limit).Find(&list).Error
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
		return domains.ModelMapping{}, errors.New(domains.ErrorModelNotSupported)
	}
	return model, err
}

func (s ModelService) SetEnabled(guid string, enabled bool) error {
	return global.NAV_DB.Model(&domains.ModelMapping{}).Where("guid = ?", guid).Update("enabled", enabled).Error
}

func (s ModelService) Delete(guid string) error {
	err := global.NAV_DB.Where("guid = ?", guid).Delete(&domains.ModelMapping{}).Error
	AuditServiceApp.Record("", "model.delete", "model", guid, nil)
	return err
}
