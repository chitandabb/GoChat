package v1

import (
	"github.com/gin-gonic/gin"
	"gochat/internal/dto/request"
	"gochat/internal/service/gorm"
)

// GetUserList 获取联系人列表（身份来自已认证用户）
func GetUserList(c *gin.Context) {
	userList, err := gorm.UserContactService.GetUserList(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, userList)
}

// LoadMyJoinedGroup 获取我加入的群聊
func LoadMyJoinedGroup(c *gin.Context) {
	groupList, err := gorm.UserContactService.LoadMyJoinedGroup(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, groupList)
}

// GetContactInfo 获取联系人信息
func GetContactInfo(c *gin.Context) {
	var getContactInfoReq request.GetContactInfoRequest
	if !BindJSON(c, &getContactInfoReq) {
		return
	}
	contactInfo, err := gorm.UserContactService.GetContactInfo(getContactInfoReq.ContactId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, contactInfo)
}

// DeleteContact 删除联系人
func DeleteContact(c *gin.Context) {
	var deleteContactReq request.DeleteContactRequest
	if !BindJSON(c, &deleteContactReq) {
		return
	}
	if err := gorm.UserContactService.DeleteContact(AuthUUID(c), deleteContactReq.ContactId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// ApplyContact 申请添加联系人
func ApplyContact(c *gin.Context) {
	var applyContactReq request.ApplyContactRequest
	if !BindJSON(c, &applyContactReq) {
		return
	}
	applyContactReq.OwnerId = AuthUUID(c)
	if err := gorm.UserContactService.ApplyContact(applyContactReq); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// GetNewContactList 获取新的联系人申请列表
func GetNewContactList(c *gin.Context) {
	data, err := gorm.UserContactService.GetNewContactList(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, data)
}

// PassContactApply 通过联系人申请
func PassContactApply(c *gin.Context) {
	var passContactApplyReq request.PassContactApplyRequest
	if !BindJSON(c, &passContactApplyReq) {
		return
	}
	if err := gorm.UserContactService.PassContactApply(AuthUUID(c), passContactApplyReq.ContactId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// RefuseContactApply 拒绝联系人申请
func RefuseContactApply(c *gin.Context) {
	var passContactApplyReq request.PassContactApplyRequest
	if !BindJSON(c, &passContactApplyReq) {
		return
	}
	if err := gorm.UserContactService.RefuseContactApply(AuthUUID(c), passContactApplyReq.ContactId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// BlackContact 拉黑联系人
func BlackContact(c *gin.Context) {
	var req request.BlackContactRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserContactService.BlackContact(AuthUUID(c), req.ContactId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// CancelBlackContact 解除拉黑联系人
func CancelBlackContact(c *gin.Context) {
	var req request.BlackContactRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserContactService.CancelBlackContact(AuthUUID(c), req.ContactId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// GetAddGroupList 获取新的群聊申请列表
func GetAddGroupList(c *gin.Context) {
	var req request.AddGroupListRequest
	if !BindJSON(c, &req) {
		return
	}
	data, err := gorm.UserContactService.GetAddGroupList(req.GroupId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, data)
}

// BlackApply 拉黑申请
func BlackApply(c *gin.Context) {
	var req request.BlackApplyRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserContactService.BlackApply(AuthUUID(c), req.ContactId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}
