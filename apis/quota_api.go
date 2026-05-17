package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type QuotaApi struct{}

func (a QuotaApi) Upsert(c *gin.Context) {
	var input services.QuotaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if input.AccountGuid == "" {
		input.AccountGuid = c.Param("guid")
	}
	quota, err := quotaService.Upsert(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(quota, c)
}

func (a QuotaApi) List(c *gin.Context) {
	list, err := quotaService.List(c.Query("accountGuid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}

func (a QuotaApi) ListByAccount(c *gin.Context) {
	list, err := quotaService.List(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}
