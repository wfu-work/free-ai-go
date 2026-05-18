package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type AccountGroupApi struct{}

func (a AccountGroupApi) Create(c *gin.Context) {
	var input services.AccountGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	group, err := accountGroupService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(group, c)
}

func (a AccountGroupApi) List(c *gin.Context) {
	list, err := accountGroupService.List()
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}

func (a AccountGroupApi) Detail(c *gin.Context) {
	group, err := accountGroupService.Get(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(group, c)
}

func (a AccountGroupApi) Update(c *gin.Context) {
	var input services.AccountGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	group, err := accountGroupService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(group, c)
}

func (a AccountGroupApi) Delete(c *gin.Context) {
	if err := accountGroupService.Delete(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
