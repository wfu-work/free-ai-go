package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type AccountApi struct{}

func (a AccountApi) Create(c *gin.Context) {
	var input services.CreateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	account, err := accountService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

func (a AccountApi) Update(c *gin.Context) {
	var input services.CreateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	account, err := accountService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

func (a AccountApi) List(c *gin.Context) {
	list, err := accountService.List(200)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}

func (a AccountApi) Detail(c *gin.Context) {
	account, err := accountService.Get(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

func (a AccountApi) Delete(c *gin.Context) {
	if err := accountService.Delete(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

func (a AccountApi) Enable(c *gin.Context) {
	if err := accountService.SetEnabled(c.Param("guid"), true); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

func (a AccountApi) Disable(c *gin.Context) {
	if err := accountService.SetEnabled(c.Param("guid"), false); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

func (a AccountApi) Refresh(c *gin.Context) {
	account, err := accountService.Refresh(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

func (a AccountApi) Test(c *gin.Context) {
	var input services.AccountTestInput
	_ = c.ShouldBindJSON(&input)
	result, err := accountService.Test(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

func (a AccountApi) FetchModels(c *gin.Context) {
	var input services.FetchAccountModelsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	models, err := accountService.FetchModels(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(gin.H{"models": models}, c)
}

func (a AccountApi) Reorder(c *gin.Context) {
	var input services.ReorderAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := accountService.Reorder(input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
