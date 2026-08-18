package gorm

import (
	"encoding/json"
	"errors"
	"fmt"
	"gochat/internal/dao"
	"gochat/internal/dto/request"
	"gochat/internal/dto/respond"
	"gochat/internal/model"
	myredis "gochat/internal/service/redis"
	"gochat/pkg/apperr"
	"gochat/pkg/constants"
	"gochat/pkg/enum/contact/contact_status_enum"
	"gochat/pkg/enum/contact/contact_type_enum"
	"gochat/pkg/enum/contact_apply/contact_apply_status_enum"
	"gochat/pkg/enum/group_info/group_status_enum"
	"gochat/pkg/enum/user_info/user_status_enum"
	"gochat/pkg/util/random"
	"gochat/pkg/zlog"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type userContactService struct {
}

var UserContactService = new(userContactService)

// GetUserList 获取用户列表
// 关于用户被禁用的问题，这里查到的是所有联系人，如果被禁用或被拉黑会以弹窗的形式提醒，无法打开会话框；如果被删除，是搜索不到该联系人的。
func (u *userContactService) GetUserList(ownerId string) ([]respond.MyUserListRespond, error) {
	rspString, err := myredis.GetKeyNilIsErr("contact_user_list_" + ownerId)
	if err != nil {
		if errors.Is(err, redis.Nil) {

			// dao
			var contactList []model.UserContact
			// 没有被删除
			if res := dao.GormDB.Order("created_at DESC").Where("user_id = ? AND status != 4", ownerId).Find(&contactList); res.Error != nil {
				// 不存在不是业务问题，用Info，return 0
				if errors.Is(res.Error, gorm.ErrRecordNotFound) {
					zlog.Info("目前不存在联系人")
					return []respond.MyUserListRespond{}, nil
				} else {
					zlog.Error(res.Error.Error())
					return nil, apperr.SystemError(res.Error)
				}
			}
			// dto
			var userListRsp []respond.MyUserListRespond
			for _, contact := range contactList {
				// 联系人中是用户的
				if contact.ContactType == contact_type_enum.USER {
					// 获取用户信息
					var user model.UserInfo
					if res := dao.GormDB.First(&user, "uuid = ?", contact.ContactId); res.Error != nil {
						// 肯定是存在的，不可能无缘无故删掉，目前不用加notfound的判断
						zlog.Error(res.Error.Error())
						return nil, apperr.SystemError(res.Error)
					}
					userListRsp = append(userListRsp, respond.MyUserListRespond{
						UserId:   user.Uuid,
						UserName: user.Nickname,
						Avatar:   user.Avatar,
					})
				}
			}
			if userListRsp == nil {
				userListRsp = []respond.MyUserListRespond{}
			}
			rspString, err := json.Marshal(userListRsp)
			if err != nil {
				zlog.Error(err.Error())
			}
			if err := myredis.SetKeyExJitter("contact_user_list_"+ownerId, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
				zlog.Error(err.Error())
			}
			return userListRsp, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp []respond.MyUserListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return rsp, nil
}

// LoadMyJoinedGroup 获取我加入的群聊
func (u *userContactService) LoadMyJoinedGroup(ownerId string) ([]respond.LoadMyJoinedGroupRespond, error) {
	rspString, err := myredis.GetKeyNilIsErr("my_joined_group_list_" + ownerId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var contactList []model.UserContact
			// 没有退群，也没有被踢出群聊
			if res := dao.GormDB.Order("created_at DESC").Where("user_id = ? AND status != 6 AND status != 7", ownerId).Find(&contactList); res.Error != nil {
				// 不存在不是业务问题，用Info，return 0
				if errors.Is(res.Error, gorm.ErrRecordNotFound) {
					zlog.Info("目前不存在加入的群聊")
					return []respond.LoadMyJoinedGroupRespond{}, nil
				} else {
					zlog.Error(res.Error.Error())
					return nil, apperr.SystemError(res.Error)
				}
			}
			var groupList []model.GroupInfo
			for _, contact := range contactList {
				if contact.ContactId[0] == 'G' {
					// 获取群聊信息
					var group model.GroupInfo
					if res := dao.GormDB.First(&group, "uuid = ?", contact.ContactId); res.Error != nil {
						zlog.Error(res.Error.Error())
						return nil, apperr.SystemError(res.Error)
					}
					// 群没被删除，同时群主不是自己
					// 群主删除或admin删除群聊，status为7，即被踢出群聊，所以不用判断群是否被删除，删除了到不了这步
					if group.OwnerId != ownerId {
						groupList = append(groupList, group)
					}
				}
			}
			var groupListRsp []respond.LoadMyJoinedGroupRespond
			for _, group := range groupList {
				groupListRsp = append(groupListRsp, respond.LoadMyJoinedGroupRespond{
					GroupId:   group.Uuid,
					GroupName: group.Name,
					Avatar:    group.Avatar,
				})
			}
			if groupListRsp == nil {
				groupListRsp = []respond.LoadMyJoinedGroupRespond{}
			}
			rspString, err := json.Marshal(groupListRsp)
			if err != nil {
				zlog.Error(err.Error())
			}
			if err := myredis.SetKeyExJitter("my_joined_group_list_"+ownerId, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
				zlog.Error(err.Error())
			}
			return groupListRsp, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp []respond.LoadMyJoinedGroupRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return rsp, nil
}

// GetContactInfo 获取联系人信息
// 调用这个接口的前提是该联系人没有处在删除或被删除，或者该用户还在群聊中
// redis todo
func (u *userContactService) GetContactInfo(contactId string) (*respond.GetContactInfoRespond, error) {
	if contactId[0] == 'G' {
		var group model.GroupInfo
		if res := dao.GormDB.First(&group, "uuid = ?", contactId); res.Error != nil {
			zlog.Error(res.Error.Error())
			return nil, apperr.SystemError(res.Error)
		}
		// 没被禁用
		if group.Status != group_status_enum.DISABLE {
			return &respond.GetContactInfoRespond{
				ContactId:        group.Uuid,
				ContactName:      group.Name,
				ContactAvatar:    group.Avatar,
				ContactNotice:    group.Notice,
				ContactAddMode:   group.AddMode,
				ContactMembers:   group.Members,
				ContactMemberCnt: group.MemberCnt,
				ContactOwnerId:   group.OwnerId,
			}, nil
		} else {
			zlog.Error("该群聊处于禁用状态")
			return nil, apperr.Biz("该群聊处于禁用状态")
		}
	} else {
		var user model.UserInfo
		if res := dao.GormDB.First(&user, "uuid = ?", contactId); res.Error != nil {
			zlog.Error(res.Error.Error())
			return nil, apperr.SystemError(res.Error)
		}
		log.Println(user)
		if user.Status != user_status_enum.DISABLE {
			return &respond.GetContactInfoRespond{
				ContactId:        user.Uuid,
				ContactName:      user.Nickname,
				ContactAvatar:    user.Avatar,
				ContactBirthday:  user.Birthday,
				ContactEmail:     user.Email,
				ContactPhone:     user.Telephone,
				ContactGender:    user.Gender,
				ContactSignature: user.Signature,
			}, nil
		} else {
			zlog.Info("该用户处于禁用状态")
			return nil, apperr.Biz("该用户处于禁用状态")
		}
	}
}

// DeleteContact 删除联系人（只包含用户）
func (u *userContactService) DeleteContact(ownerId, contactId string) error {
	// status改变为删除
	var deletedAt gorm.DeletedAt
	deletedAt.Time = time.Now()
	deletedAt.Valid = true
	if res := dao.GormDB.Model(&model.UserContact{}).Where("user_id = ? AND contact_id = ?", ownerId, contactId).Updates(map[string]interface{}{
		"deleted_at": deletedAt,
		"status":     contact_status_enum.DELETE,
	}); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	if res := dao.GormDB.Model(&model.UserContact{}).Where("user_id = ? AND contact_id = ?", contactId, ownerId).Updates(map[string]interface{}{
		"deleted_at": deletedAt,
		"status":     contact_status_enum.BE_DELETE,
	}); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	if res := dao.GormDB.Model(&model.Session{}).Where("send_id = ? AND receive_id = ?", ownerId, contactId).Update("deleted_at", deletedAt); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	if res := dao.GormDB.Model(&model.Session{}).Where("send_id = ? AND receive_id = ?", contactId, ownerId).Update("deleted_at", deletedAt); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	// 联系人添加的记录得删，这样之后再添加就看新的申请记录，如果申请记录结果是拉黑就没法再添加，如果是拒绝可以再添加
	if res := dao.GormDB.Model(&model.ContactApply{}).Where("contact_id = ? AND user_id = ?", ownerId, contactId).Update("deleted_at", deletedAt); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if res := dao.GormDB.Model(&model.ContactApply{}).Where("contact_id = ? AND user_id = ?", contactId, ownerId).Update("deleted_at", deletedAt); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	// 删除联系人影响双方的好友列表缓存（ownerId 与 contactId 各有一份列表）
	if err := myredis.DelKeysWithPattern("contact_user_list_" + ownerId); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("contact_user_list_" + contactId); err != nil {
		zlog.Error(err.Error())
	}
	return nil
}

// ApplyContact 申请添加联系人
func (u *userContactService) ApplyContact(req request.ApplyContactRequest) error {
	if req.ContactId[0] == 'U' {
		var user model.UserInfo
		if res := dao.GormDB.First(&user, "uuid = ?", req.ContactId); res.Error != nil {
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				zlog.Error("用户不存在")
				return apperr.Biz("用户不存在")
			} else {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}

		if user.Status == user_status_enum.DISABLE {
			zlog.Info("用户已被禁用")
			return apperr.Biz("用户已被禁用")
		}
		var contactApply model.ContactApply
		if res := dao.GormDB.Where("user_id = ? AND contact_id = ?", req.OwnerId, req.ContactId).First(&contactApply); res.Error != nil {
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				contactApply = model.ContactApply{
					Uuid:        fmt.Sprintf("A%s", random.GetNowAndLenRandomString(11)),
					UserId:      req.OwnerId,
					ContactId:   req.ContactId,
					ContactType: contact_type_enum.USER,
					Status:      contact_apply_status_enum.PENDING,
					Message:     req.Message,
					LastApplyAt: time.Now(),
				}
				if res := dao.GormDB.Create(&contactApply); res.Error != nil {
					zlog.Error(res.Error.Error())
					return apperr.SystemError(res.Error)
				}
			} else {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
		// 如果存在申请记录，先看看有没有被拉黑
		if contactApply.Status == contact_apply_status_enum.BLACK {
			return apperr.Biz("对方已将你拉黑")
		}
		contactApply.LastApplyAt = time.Now()
		contactApply.Status = contact_apply_status_enum.PENDING

		if res := dao.GormDB.Save(&contactApply); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		return nil
	} else if req.ContactId[0] == 'G' {
		var group model.GroupInfo
		if res := dao.GormDB.First(&group, "uuid = ?", req.ContactId); res.Error != nil {
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				zlog.Error("群聊不存在")
				return apperr.Biz("群聊不存在")
			} else {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
		if group.Status == group_status_enum.DISABLE {
			zlog.Info("群聊已被禁用")
			return apperr.Biz("群聊已被禁用")
		}
		var contactApply model.ContactApply
		if res := dao.GormDB.Where("user_id = ? AND contact_id = ?", req.OwnerId, req.ContactId).First(&contactApply); res.Error != nil {
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				contactApply = model.ContactApply{
					Uuid:        fmt.Sprintf("A%s", random.GetNowAndLenRandomString(11)),
					UserId:      req.OwnerId,
					ContactId:   req.ContactId,
					ContactType: contact_type_enum.GROUP,
					Status:      contact_apply_status_enum.PENDING,
					Message:     req.Message,
					LastApplyAt: time.Now(),
				}
				if res := dao.GormDB.Create(&contactApply); res.Error != nil {
					zlog.Error(res.Error.Error())
					return apperr.SystemError(res.Error)
				}
			} else {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
		contactApply.LastApplyAt = time.Now()

		if res := dao.GormDB.Save(&contactApply); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		return nil
	} else {
		return apperr.Biz("用户/群聊不存在")
	}

}

// GetNewContactList 获取新的联系人申请列表
func (u *userContactService) GetNewContactList(ownerId string) ([]respond.NewContactListRespond, error) {
	var contactApplyList []model.ContactApply
	if res := dao.GormDB.Where("contact_id = ? AND status = ?", ownerId, contact_apply_status_enum.PENDING).Find(&contactApplyList); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			zlog.Info("没有在申请的联系人")
			return []respond.NewContactListRespond{}, nil
		} else {
			zlog.Error(res.Error.Error())
			return nil, apperr.SystemError(res.Error)
		}
	}
	var rsp []respond.NewContactListRespond
	// 所有contact都没被删除
	for _, contactApply := range contactApplyList {
		var message string
		if contactApply.Message == "" {
			message = "申请理由：无"
		} else {
			message = "申请理由：" + contactApply.Message
		}
		newContact := respond.NewContactListRespond{
			ContactId: contactApply.Uuid,
			Message:   message,
		}
		var user model.UserInfo
		if res := dao.GormDB.First(&user, "uuid = ?", contactApply.UserId); res.Error != nil {
			return nil, apperr.SystemError(res.Error)
		}
		newContact.ContactId = user.Uuid
		newContact.ContactName = user.Nickname
		newContact.ContactAvatar = user.Avatar
		rsp = append(rsp, newContact)
	}
	if rsp == nil {
		rsp = []respond.NewContactListRespond{}
	}
	return rsp, nil
}

// GetAddGroupList 获取新的加群列表
// 前端已经判断调用接口的用户是群主，也只有群主才能调用这个接口
func (u *userContactService) GetAddGroupList(groupId string) ([]respond.AddGroupListRespond, error) {
	var contactApplyList []model.ContactApply
	if res := dao.GormDB.Where("contact_id = ? AND status = ?", groupId, contact_apply_status_enum.PENDING).Find(&contactApplyList); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			zlog.Info("没有在申请的联系人")
			return []respond.AddGroupListRespond{}, nil
		} else {
			zlog.Error(res.Error.Error())
			return nil, apperr.SystemError(res.Error)
		}
	}
	var rsp []respond.AddGroupListRespond
	for _, contactApply := range contactApplyList {
		var message string
		if contactApply.Message == "" {
			message = "申请理由：无"
		} else {
			message = "申请理由：" + contactApply.Message
		}
		newContact := respond.AddGroupListRespond{
			ContactId: contactApply.Uuid,
			Message:   message,
		}
		var user model.UserInfo
		if res := dao.GormDB.First(&user, "uuid = ?", contactApply.UserId); res.Error != nil {
			return nil, apperr.SystemError(res.Error)
		}
		newContact.ContactId = user.Uuid
		newContact.ContactName = user.Nickname
		newContact.ContactAvatar = user.Avatar
		rsp = append(rsp, newContact)
	}
	if rsp == nil {
		rsp = []respond.AddGroupListRespond{}
	}
	return rsp, nil
}

// PassContactApply 通过联系人/加群申请。
// operatorId 为已认证操作者，ownerId 为被申请方（用户 uuid 或群 id，G 开头）。
// 用户场景只允许处理"本人收到的申请"；群场景只允许群主审核，防止越权。
func (u *userContactService) PassContactApply(operatorId, ownerId, contactId string) error {
	if err := u.checkContactApplyOwner(operatorId, ownerId); err != nil {
		return err
	}

	var contactApply model.ContactApply
	if res := dao.GormDB.Where("contact_id = ? AND user_id = ?", ownerId, contactId).First(&contactApply); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if ownerId[0] == 'U' {
		var user model.UserInfo
		if res := dao.GormDB.Where("uuid = ?", contactId).Find(&user); res.Error != nil {
			zlog.Error(res.Error.Error())
		}
		if user.Status == user_status_enum.DISABLE {
			zlog.Error("用户已被禁用")
			return apperr.Biz("用户已被禁用")
		}
		contactApply.Status = contact_apply_status_enum.AGREE
		if res := dao.GormDB.Save(&contactApply); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		newContact := model.UserContact{
			UserId:      ownerId,
			ContactId:   contactId,
			ContactType: contact_type_enum.USER,     // 用户
			Status:      contact_status_enum.NORMAL, // 正常
			CreatedAt:   time.Now(),
			UpdateAt:    time.Now(),
		}
		if res := dao.GormDB.Create(&newContact); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		anotherContact := model.UserContact{
			UserId:      contactId,
			ContactId:   ownerId,
			ContactType: contact_type_enum.USER,     // 用户
			Status:      contact_status_enum.NORMAL, // 正常
			CreatedAt:   newContact.CreatedAt,
			UpdateAt:    newContact.UpdateAt,
		}
		if res := dao.GormDB.Create(&anotherContact); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		// 好友关系建立后，双方的好友列表缓存都要失效（各自缓存一份自己的列表）
		if err := myredis.DelKeysWithPattern("contact_user_list_" + ownerId); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("contact_user_list_" + contactId); err != nil {
			zlog.Error(err.Error())
		}
		return nil
	} else {
		var group model.GroupInfo
		if res := dao.GormDB.Where("uuid = ?", ownerId).Find(&group); res.Error != nil {
			zlog.Error(res.Error.Error())
		}
		if group.Status == group_status_enum.DISABLE {
			zlog.Error("群聊已被禁用")
			return apperr.Biz("群聊已被禁用")
		}
		contactApply.Status = contact_apply_status_enum.AGREE
		if res := dao.GormDB.Save(&contactApply); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		// 群聊就只用创建一个UserContact，因为一个UserContact足以表达双方的状态
		newContact := model.UserContact{
			UserId:      contactId,
			ContactId:   ownerId,
			ContactType: contact_type_enum.GROUP,    // 用户
			Status:      contact_status_enum.NORMAL, // 正常
			CreatedAt:   time.Now(),
			UpdateAt:    time.Now(),
		}
		if res := dao.GormDB.Create(&newContact); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		var members []string
		if err := json.Unmarshal(group.Members, &members); err != nil {
			zlog.Error(err.Error())
			return apperr.SystemError(err)
		}
		members = append(members, contactId)
		group.MemberCnt = len(members)
		group.Members, _ = json.Marshal(members)
		if res := dao.GormDB.Save(&group); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		// 入群后，申请者（contactId）的"已加入群列表"缓存需要失效；用户维度缓存 key。
		if err := myredis.DelKeysWithPattern("my_joined_group_list_" + contactId); err != nil {
			zlog.Error(err.Error())
		}
		// 群主侧的群会话列表也失效（会话列表含群名/头像，保持一致性）
		if err := myredis.DelKeysWithPattern("group_session_list_" + group.OwnerId); err != nil {
			zlog.Error(err.Error())
		}
		return nil
	}
}

// checkContactApplyOwner 校验操作者是否有权处理该申请：
//   - 用户申请：owner 必须是操作者本人；
//   - 加群申请：owner 为群 id，操作者必须是该群群主（仅群主可审核）。
func (u *userContactService) checkContactApplyOwner(operatorId, ownerId string) error {
	if ownerId == "" {
		return apperr.BadRequest("参数错误")
	}
	if ownerId[0] == 'U' {
		if ownerId != operatorId {
			return apperr.Forbidden("无权限处理该申请")
		}
		return nil
	}
	if ownerId[0] == 'G' {
		var group model.GroupInfo
		if res := dao.GormDB.Where("uuid = ?", ownerId).First(&group); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		if group.OwnerId != operatorId {
			return apperr.Forbidden("仅群主可处理加群申请")
		}
		return nil
	}
	return apperr.Biz("用户/群聊不存在")
}

// RefuseContactApply 拒绝联系人/加群申请（身份校验同 PassContactApply）。
func (u *userContactService) RefuseContactApply(operatorId, ownerId, contactId string) error {
	if err := u.checkContactApplyOwner(operatorId, ownerId); err != nil {
		return err
	}
	var contactApply model.ContactApply
	if res := dao.GormDB.Where("contact_id = ? AND user_id = ?", ownerId, contactId).First(&contactApply); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	contactApply.Status = contact_apply_status_enum.REFUSE
	if res := dao.GormDB.Save(&contactApply); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	return nil
}

// BlackContact 拉黑联系人
func (u *userContactService) BlackContact(ownerId string, contactId string) error {
	// 拉黑
	if res := dao.GormDB.Model(&model.UserContact{}).Where("user_id = ? AND contact_id = ?", ownerId, contactId).Updates(map[string]interface{}{
		"status":    contact_status_enum.BLACK,
		"update_at": time.Now(),
	}); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	// 被拉黑
	if res := dao.GormDB.Model(&model.UserContact{}).Where("user_id = ? AND contact_id = ?", contactId, ownerId).Updates(map[string]interface{}{
		"status":    contact_status_enum.BE_BLACK,
		"update_at": time.Now(),
	}); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	// 删除会话
	var deletedAt gorm.DeletedAt
	deletedAt.Time = time.Now()
	deletedAt.Valid = true
	if res := dao.GormDB.Model(&model.Session{}).Where("send_id = ? AND receive_id = ?", ownerId, contactId).Update("deleted_at", deletedAt); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	return nil
}

// CancelBlackContact 取消拉黑联系人
func (u *userContactService) CancelBlackContact(ownerId string, contactId string) error {
	// 因为前端的设定，这里需要判断一下ownerId和contactId是不是有拉黑和被拉黑的状态
	var blackContact model.UserContact
	if res := dao.GormDB.Where("user_id = ? AND contact_id = ?", ownerId, contactId).First(&blackContact); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if blackContact.Status != contact_status_enum.BLACK {
		return apperr.Biz("未拉黑该联系人，无需解除拉黑")
	}
	var beBlackContact model.UserContact
	if res := dao.GormDB.Where("user_id = ? AND contact_id = ?", contactId, ownerId).First(&beBlackContact); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if beBlackContact.Status != contact_status_enum.BE_BLACK {
		return apperr.Biz("该联系人未被拉黑，无需解除拉黑")
	}

	// 取消拉黑
	blackContact.Status = contact_status_enum.NORMAL
	beBlackContact.Status = contact_status_enum.NORMAL
	if res := dao.GormDB.Save(&blackContact); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if res := dao.GormDB.Save(&beBlackContact); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	return nil
}

// BlackApply 拉黑申请（身份校验同 PassContactApply）
func (u *userContactService) BlackApply(operatorId, ownerId, contactId string) error {
	if err := u.checkContactApplyOwner(operatorId, ownerId); err != nil {
		return err
	}
	var contactApply model.ContactApply
	if res := dao.GormDB.Where("contact_id = ? AND user_id = ?", ownerId, contactId).First(&contactApply); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	contactApply.Status = contact_apply_status_enum.BLACK
	if res := dao.GormDB.Save(&contactApply); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	return nil
}
