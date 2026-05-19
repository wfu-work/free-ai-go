package services

import (
	"errors"
	"fmt"
	"time"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const CoreBackupVersion = "freeai-core-backup/v1"

type BackupService struct{}

var BackupServiceApp = BackupService{}

type CoreBackupAccount struct {
	domains.Account
	EncryptedSecret string `json:"encryptedSecret,omitempty"`
}

type CoreBackupPlatformKey struct {
	domains.PlatformKey
	KeyHash      string `json:"keyHash,omitempty"`
	EncryptedKey string `json:"encryptedKey,omitempty"`
}

type CoreBackupPayload struct {
	Version       string                  `json:"version"`
	ExportedAt    int64                   `json:"exportedAt"`
	GatewayConfig GatewayProxyConfigInput `json:"gatewayConfig"`
	Data          CoreBackupData          `json:"data"`
}

type CoreBackupData struct {
	Accounts      []CoreBackupAccount     `json:"accounts"`
	AccountGroups []domains.AccountGroup  `json:"accountGroups"`
	AccountQuotas []domains.AccountQuota  `json:"accountQuotas"`
	ModelMappings []domains.ModelMapping  `json:"modelMappings"`
	PlatformKeys  []CoreBackupPlatformKey `json:"platformKeys"`
	RouteStates   []domains.RouteState    `json:"routeStates"`
}

type CoreBackupImportResult struct {
	Success             int      `json:"success"`
	Failed              int      `json:"failed"`
	Accounts            int      `json:"accounts"`
	FailedAccounts      int      `json:"failedAccounts"`
	AccountGroups       int      `json:"accountGroups"`
	FailedAccountGroups int      `json:"failedAccountGroups"`
	AccountQuotas       int      `json:"accountQuotas"`
	FailedAccountQuotas int      `json:"failedAccountQuotas"`
	ModelMappings       int      `json:"modelMappings"`
	FailedModelMappings int      `json:"failedModelMappings"`
	PlatformKeys        int      `json:"platformKeys"`
	FailedPlatformKeys  int      `json:"failedPlatformKeys"`
	RouteStates         int      `json:"routeStates"`
	FailedRouteStates   int      `json:"failedRouteStates"`
	GatewayConfig       int      `json:"gatewayConfig"`
	FailedGatewayConfig int      `json:"failedGatewayConfig"`
	Errors              []string `json:"errors,omitempty"`
}

func (s BackupService) ExportCore() (CoreBackupPayload, error) {
	payload := CoreBackupPayload{
		Version:       CoreBackupVersion,
		ExportedAt:    time.Now().UnixMilli(),
		GatewayConfig: GatewayProxyConfig(),
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.AccountGroups).Error; err != nil {
		return payload, err
	}
	var accounts []domains.Account
	if err := global.NAV_DB.Order("id asc").Find(&accounts).Error; err != nil {
		return payload, err
	}
	payload.Data.Accounts = make([]CoreBackupAccount, 0, len(accounts))
	for _, account := range accounts {
		payload.Data.Accounts = append(payload.Data.Accounts, CoreBackupAccount{
			Account:         account,
			EncryptedSecret: account.EncryptedSecret,
		})
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.AccountQuotas).Error; err != nil {
		return payload, err
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.ModelMappings).Error; err != nil {
		return payload, err
	}
	var platformKeys []domains.PlatformKey
	if err := global.NAV_DB.Order("id asc").Find(&platformKeys).Error; err != nil {
		return payload, err
	}
	payload.Data.PlatformKeys = make([]CoreBackupPlatformKey, 0, len(platformKeys))
	for _, key := range platformKeys {
		payload.Data.PlatformKeys = append(payload.Data.PlatformKeys, CoreBackupPlatformKey{
			PlatformKey:  key,
			KeyHash:      key.KeyHash,
			EncryptedKey: key.EncryptedKey,
		})
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.RouteStates).Error; err != nil {
		return payload, err
	}
	return payload, nil
}

func (s BackupService) ImportCore(payload CoreBackupPayload) (CoreBackupImportResult, error) {
	if payload.Version != CoreBackupVersion {
		return CoreBackupImportResult{}, errors.New("unsupported backup file version")
	}
	result := CoreBackupImportResult{}
	err := global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		accountGroups := make([]domains.AccountGroup, 0, len(payload.Data.AccountGroups))
		for _, item := range payload.Data.AccountGroups {
			item.Id = 0
			accountGroups = append(accountGroups, item)
		}
		result.AccountGroups, result.FailedAccountGroups = upsertByGuidSkipping(tx, accountGroups, "账号分组", &result.Errors)

		accounts := make([]domains.Account, 0, len(payload.Data.Accounts))
		for _, item := range payload.Data.Accounts {
			account := item.Account
			account.Id = 0
			account.EncryptedSecret = item.EncryptedSecret
			accounts = append(accounts, account)
		}
		result.Accounts, result.FailedAccounts = upsertByGuidSkipping(tx, accounts, "账号", &result.Errors)

		accountQuotas := make([]domains.AccountQuota, 0, len(payload.Data.AccountQuotas))
		for _, item := range payload.Data.AccountQuotas {
			item.Id = 0
			accountQuotas = append(accountQuotas, item)
		}
		result.AccountQuotas, result.FailedAccountQuotas = upsertByGuidSkipping(tx, accountQuotas, "账号额度", &result.Errors)

		modelMappings := make([]domains.ModelMapping, 0, len(payload.Data.ModelMappings))
		for _, item := range payload.Data.ModelMappings {
			item.Id = 0
			modelMappings = append(modelMappings, item)
		}
		result.ModelMappings, result.FailedModelMappings = upsertByGuidSkipping(tx, modelMappings, "模型映射", &result.Errors)

		platformKeys := make([]domains.PlatformKey, 0, len(payload.Data.PlatformKeys))
		for _, item := range payload.Data.PlatformKeys {
			key := item.PlatformKey
			key.Id = 0
			key.KeyHash = item.KeyHash
			key.EncryptedKey = item.EncryptedKey
			platformKeys = append(platformKeys, key)
		}
		result.PlatformKeys, result.FailedPlatformKeys = upsertByGuidSkipping(tx, platformKeys, "平台密钥", &result.Errors)

		routeStates := make([]domains.RouteState, 0, len(payload.Data.RouteStates))
		for _, item := range payload.Data.RouteStates {
			item.Id = 0
			routeStates = append(routeStates, item)
		}
		result.RouteStates, result.FailedRouteStates = upsertByGuidSkipping(tx, routeStates, "路由状态", &result.Errors)

		return nil
	})
	if err != nil {
		return CoreBackupImportResult{}, err
	}
	if _, err := UpdateGatewayProxyConfig(payload.GatewayConfig); err != nil {
		result.FailedGatewayConfig = 1
		appendImportError(&result.Errors, fmt.Sprintf("网关配置: %v", err))
	} else {
		result.GatewayConfig = 1
	}
	result.Success = result.AccountGroups + result.Accounts + result.AccountQuotas + result.ModelMappings + result.PlatformKeys + result.RouteStates + result.GatewayConfig
	result.Failed = result.FailedAccountGroups + result.FailedAccounts + result.FailedAccountQuotas + result.FailedModelMappings + result.FailedPlatformKeys + result.FailedRouteStates + result.FailedGatewayConfig
	return result, nil
}

func upsertByGuidSkipping[T any](tx *gorm.DB, items []T, label string, errors *[]string) (int, int) {
	success := 0
	failed := 0
	for index, item := range items {
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "guid"}},
			UpdateAll: true,
		}).Create(&item).Error
		if err != nil {
			failed++
			appendImportError(errors, fmt.Sprintf("%s第%d条: %v", label, index+1, err))
			continue
		}
		success++
	}
	return success, failed
}

func appendImportError(errors *[]string, message string) {
	if len(*errors) >= 10 {
		return
	}
	*errors = append(*errors, message)
}
