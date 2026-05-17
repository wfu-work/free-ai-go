package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type ModelApi struct{}

func (a ModelApi) Create(c *gin.Context) {
	var input services.ModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	model, err := modelService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(model, c)
}

func (a ModelApi) List(c *gin.Context) {
	list, err := modelService.List(200)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}

func (a ModelApi) Detail(c *gin.Context) {
	model, err := modelService.Get(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(model, c)
}

func (a ModelApi) Update(c *gin.Context) {
	var input services.ModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	model, err := modelService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(model, c)
}

func (a ModelApi) Delete(c *gin.Context) {
	if err := modelService.Delete(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

func (a ModelApi) Enable(c *gin.Context) {
	if err := modelService.SetEnabled(c.Param("guid"), true); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

func (a ModelApi) Disable(c *gin.Context) {
	if err := modelService.SetEnabled(c.Param("guid"), false); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
