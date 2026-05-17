package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type PlatformKeyApi struct{}

func (a PlatformKeyApi) Create(c *gin.Context) {
	var input services.CreatePlatformKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	out, err := platformKeyService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(out, c)
}

func (a PlatformKeyApi) List(c *gin.Context) {
	list, err := platformKeyService.List(200)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}

func (a PlatformKeyApi) Detail(c *gin.Context) {
	key, err := platformKeyService.Get(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(key, c)
}

func (a PlatformKeyApi) Update(c *gin.Context) {
	var input services.CreatePlatformKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	key, err := platformKeyService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(key, c)
}

func (a PlatformKeyApi) Delete(c *gin.Context) {
	if err := platformKeyService.Delete(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

func (a PlatformKeyApi) Enable(c *gin.Context) {
	if err := platformKeyService.SetEnabled(c.Param("guid"), true); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

func (a PlatformKeyApi) Disable(c *gin.Context) {
	if err := platformKeyService.SetEnabled(c.Param("guid"), false); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
