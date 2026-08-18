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
	"gochat/pkg/enum/group_info/group_status_enum"
	"gochat/pkg/util/random"
	"gochat/pkg/zlog"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type groupInfoService struct {
}

var GroupInfoService = new(groupInfoService)

// CreateGroup 创建群聊
func (g *groupInfoService) CreateGroup(groupReq request.CreateGroupRequest) error {
	// 先组装群信息。当前群成员仍然保存在 group_info.members 的 JSON 字段里。
	group := model.GroupInfo{
		Uuid:      fmt.Sprintf("G%s", random.GetNowAndLenRandomString(11)),
		Name:      groupReq.Name,
		Notice:    groupReq.Notice,
		OwnerId:   groupReq.OwnerId,
		MemberCnt: 1,
		AddMode:   groupReq.AddMode,
		Avatar:    groupReq.Avatar,
		Status:    group_status_enum.NORMAL,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	var members []string
	members = append(members, groupReq.OwnerId)
	var err error
	group.Members, err = json.Marshal(members)
	if err != nil {
		zlog.Error(err.Error())
		return apperr.SystemError(err)
	}
	if res := dao.GormDB.Create(&group); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	// 建群后，群主还需要在联系人表里拥有一条“我加入了这个群”的关系记录。
	contact := model.UserContact{
		UserId:      groupReq.OwnerId,
		ContactId:   group.Uuid,
		ContactType: contact_type_enum.GROUP,
		Status:      contact_status_enum.NORMAL,
		CreatedAt:   time.Now(),
		UpdateAt:    time.Now(),
	}
	if res := dao.GormDB.Create(&contact); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if err := myredis.DelKeysWithPattern("contact_mygroup_list_" + groupReq.OwnerId); err != nil {
		zlog.Error(err.Error())
	}

	return nil
}

// LoadMyGroup 获取我创建的群聊
func (g *groupInfoService) LoadMyGroup(ownerId string) ([]respond.LoadMyGroupRespond, error) {
	// 先读缓存；缓存没有命中时再从数据库回源并回填缓存。
	rspString, err := myredis.GetKeyNilIsErr("contact_mygroup_list_" + ownerId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var groupList []model.GroupInfo
			if res := dao.GormDB.Order("created_at DESC").Where("owner_id = ?", ownerId).Find(&groupList); res.Error != nil {
				zlog.Error(res.Error.Error())
				return nil, apperr.SystemError(res.Error)
			}
			var groupListRsp []respond.LoadMyGroupRespond
			for _, group := range groupList {
				groupListRsp = append(groupListRsp, respond.LoadMyGroupRespond{
					GroupId:   group.Uuid,
					GroupName: group.Name,
					Avatar:    group.Avatar,
				})
			}
			rspString, err := json.Marshal(groupListRsp)
			if err != nil {
				zlog.Error(err.Error())
			}
			if err := myredis.SetKeyExJitter("contact_mygroup_list_"+ownerId, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
				zlog.Error(err.Error())
			}
			return groupListRsp, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var groupListRsp []respond.LoadMyGroupRespond
	if err := json.Unmarshal([]byte(rspString), &groupListRsp); err != nil {
		zlog.Error(err.Error())
	}
	return groupListRsp, nil
}

// GetGroupInfo 获取群聊详情
func (g *groupInfoService) GetGroupInfo(groupId string) (*respond.GetGroupInfoRespond, error) {
	// 当前仍保留了读取 group_info_ 缓存的逻辑，但详情回填缓存暂未启用。
	rspString, err := myredis.GetKeyNilIsErr("group_info_" + groupId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var group model.GroupInfo
			if res := dao.GormDB.First(&group, "uuid = ?", groupId); res.Error != nil {
				zlog.Error(res.Error.Error())
				return nil, apperr.SystemError(res.Error)
			}
			rsp := &respond.GetGroupInfoRespond{
				Uuid:      group.Uuid,
				Name:      group.Name,
				Notice:    group.Notice,
				Avatar:    group.Avatar,
				MemberCnt: group.MemberCnt,
				OwnerId:   group.OwnerId,
				AddMode:   group.AddMode,
				Status:    group.Status,
			}
			if group.DeletedAt.Valid {
				rsp.IsDeleted = true
			} else {
				rsp.IsDeleted = false
			}
			return rsp, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp *respond.GetGroupInfoRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return rsp, nil
}

// GetGroupInfoList 获取群聊列表 - 管理员
// 管理端列表直接查库，方便同时看到已软删除群聊，不走缓存。
func (g *groupInfoService) GetGroupInfoList() ([]respond.GetGroupListRespond, error) {
	var groupList []model.GroupInfo
	if res := dao.GormDB.Unscoped().Find(&groupList); res.Error != nil {
		zlog.Error(res.Error.Error())
		return nil, apperr.SystemError(res.Error)
	}
	var rsp []respond.GetGroupListRespond
	for _, group := range groupList {
		rp := respond.GetGroupListRespond{
			Uuid:    group.Uuid,
			Name:    group.Name,
			OwnerId: group.OwnerId,
			Status:  group.Status,
		}
		if group.DeletedAt.Valid {
			rp.IsDeleted = true
		} else {
			rp.IsDeleted = false
		}
		rsp = append(rsp, rp)
	}
	return rsp, nil
}

// LeaveGroup 退群
func (g *groupInfoService) LeaveGroup(userId string, groupId string) error {
	// 1. 先把用户从群成员 JSON 中移除，并同步更新群人数。
	var group model.GroupInfo
	if res := dao.GormDB.First(&group, "uuid = ?", groupId); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	var members []string
	if err := json.Unmarshal(group.Members, &members); err != nil {
		zlog.Error(err.Error())
		return apperr.SystemError(err)
	}
	for i, member := range members {
		if member == userId {
			members = append(members[:i], members[i+1:]...)
			break
		}
	}
	if data, err := json.Marshal(members); err != nil {
		zlog.Error(err.Error())
		return apperr.SystemError(err)
	} else {
		group.Members = data
	}
	group.MemberCnt -= 1
	if res := dao.GormDB.Save(&group); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	// 2. 再把这个用户和群相关的会话、联系人、申请记录一起做软删除。
	var deletedAt gorm.DeletedAt
	deletedAt.Time = time.Now()
	deletedAt.Valid = true
	if res := dao.GormDB.Model(&model.Session{}).Where("send_id = ? AND receive_id = ?", userId, groupId).Update("deleted_at", deletedAt); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	// 删除联系人
	if res := dao.GormDB.Model(&model.UserContact{}).Where("user_id = ? AND contact_id = ?", userId, groupId).Updates(map[string]interface{}{
		"deleted_at": deletedAt,
		"status":     contact_status_enum.QUIT_GROUP, // 退群
	}); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	// 删除申请记录，后面还可以加
	if res := dao.GormDB.Model(&model.ContactApply{}).Where("contact_id = ? AND user_id = ?", groupId, userId).Update("deleted_at", deletedAt); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	// 3. 清理受影响的群会话列表、已加入群列表缓存。
	if err := myredis.DelKeysWithPattern("group_session_list_" + userId); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("my_joined_group_list_" + userId); err != nil {
		zlog.Error(err.Error())
	}
	return nil
}

// DismissGroup 解散群聊
func (g *groupInfoService) DismissGroup(ownerId, groupId string) error {
	// 解散群聊本质上是“群本身 + 关联数据”一起做软删除。
	var deletedAt gorm.DeletedAt
	deletedAt.Time = time.Now()
	deletedAt.Valid = true
	if res := dao.GormDB.Model(&model.GroupInfo{}).Where("uuid = ?", groupId).Updates(
		map[string]interface{}{
			"deleted_at": deletedAt,
			"updated_at": deletedAt.Time,
		}); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	var sessionList []model.Session
	if res := dao.GormDB.Model(&model.Session{}).Where("receive_id = ?", groupId).Find(&sessionList); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	for _, session := range sessionList {
		if res := dao.GormDB.Model(&session).Updates(
			map[string]interface{}{
				"deleted_at": deletedAt,
			}); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}

	var userContactList []model.UserContact
	if res := dao.GormDB.Model(&model.UserContact{}).Where("contact_id = ?", groupId).Find(&userContactList); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	for _, userContact := range userContactList {
		if res := dao.GormDB.Model(&userContact).Update("deleted_at", deletedAt); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}

	var contactApplys []model.ContactApply
	if res := dao.GormDB.Model(&contactApplys).Where("contact_id = ?", groupId).Find(&contactApplys); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			zlog.Info("该群没有申请记录")
		} else {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}
	for _, contactApply := range contactApplys {
		if res := dao.GormDB.Model(&contactApply).Update("deleted_at", deletedAt); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}

	// 群解散后，群主自建群列表、群会话列表，以及各成员的已加入群列表都需要失效。
	if err := myredis.DelKeysWithPattern("contact_mygroup_list_" + ownerId); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("group_session_list_" + ownerId); err != nil {
		zlog.Error(err.Error())
	}
	// 全体成员的"已加入群列表"缓存批量失效：SCAN 收集后 Pipeline 一次 RTT 删除
	invalidateAllMemberJoinedGroupCache()
	return nil
}

// invalidateAllMemberJoinedGroupCache 删除全部 my_joined_group_list_* 缓存。
func invalidateAllMemberJoinedGroupCache() {
	keys, err := myredis.ScanKeys("my_joined_group_list_*")
	if err != nil {
		zlog.Error(err.Error())
		return
	}
	if len(keys) == 0 {
		return
	}
	if err := myredis.DelKeysPipelined(keys); err != nil {
		zlog.Error(err.Error())
	}
}

// DeleteGroups 删除列表中群聊 - 管理员
func (g *groupInfoService) DeleteGroups(uuidList []string) error {
	// 管理员批量删除群聊时，会把群本身以及关联的会话、联系人、申请一起软删除。
	for _, uuid := range uuidList {
		var deletedAt gorm.DeletedAt
		deletedAt.Time = time.Now()
		deletedAt.Valid = true
		if res := dao.GormDB.Model(&model.GroupInfo{}).Where("uuid = ?", uuid).Update("deleted_at", deletedAt); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		// 删除会话
		var sessionList []model.Session
		if res := dao.GormDB.Model(&model.Session{}).Where("receive_id = ?", uuid).Find(&sessionList); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		for _, session := range sessionList {
			if res := dao.GormDB.Model(&session).Update("deleted_at", deletedAt); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
		// 删除联系人
		var userContactList []model.UserContact
		if res := dao.GormDB.Model(&model.UserContact{}).Where("contact_id = ?", uuid).Find(&userContactList); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}

		for _, userContact := range userContactList {
			if res := dao.GormDB.Model(&userContact).Update("deleted_at", deletedAt); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}

		var contactApplys []model.ContactApply
		if res := dao.GormDB.Model(&contactApplys).Where("contact_id = ?", uuid).Find(&contactApplys); res.Error != nil {
			if res.Error != gorm.ErrRecordNotFound {
				zlog.Info(res.Error.Error())
				return nil
			}
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		for _, contactApply := range contactApplys {
			if res := dao.GormDB.Model(&contactApply).Update("deleted_at", deletedAt); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
	}

	// 统一清掉群管理相关缓存，避免管理端继续看到旧数据。
	if err := myredis.DelKeysWithPrefix("contact_mygroup_list"); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPrefix("group_session_list"); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPrefix("group_session_list"); err != nil {
		zlog.Error(err.Error())
	}
	return nil
}

// CheckGroupAddMode 检查群聊加群方式
func (g *groupInfoService) CheckGroupAddMode(groupId string) (int8, error) {
	// 这个接口只关心加群方式字段，因此优先复用缓存中的群详情。
	rspString, err := myredis.GetKeyNilIsErr("group_info_" + groupId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var group model.GroupInfo
			if res := dao.GormDB.First(&group, "uuid = ?", groupId); res.Error != nil {
				zlog.Error(res.Error.Error())
				return -1, apperr.SystemError(res.Error)
			}
			return group.AddMode, nil
		} else {
			zlog.Error(err.Error())
			return -1, apperr.SystemError(err)
		}
	}
	var rsp respond.GetGroupInfoRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return rsp.AddMode, nil
}

// EnterGroupDirectly 直接进群
// ownerId 在这里实际上传的是群聊 id，contactId 是入群用户 id。
func (g *groupInfoService) EnterGroupDirectly(ownerId, contactId string) error {
	// 1. 先把用户追加到群成员 JSON 中，并同步更新群人数。
	var group model.GroupInfo
	if res := dao.GormDB.First(&group, "uuid = ?", ownerId); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	var members []string
	if err := json.Unmarshal(group.Members, &members); err != nil {
		zlog.Error(err.Error())
		return apperr.SystemError(err)
	}
	members = append(members, contactId)
	if data, err := json.Marshal(members); err != nil {
		zlog.Error(err.Error())
		return apperr.SystemError(err)
	} else {
		group.Members = data
	}
	group.MemberCnt += 1
	if res := dao.GormDB.Save(&group); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
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

	// 3. 清理新成员的会话与已加入群列表缓存（键是用户维度），让新成员能看到最新状态。
	if err := myredis.DelKeysWithPattern("group_session_list_" + contactId); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("my_joined_group_list_" + contactId); err != nil {
		zlog.Error(err.Error())
	}
	return nil
}

// SetGroupsStatus 设置群聊是否启用
func (g *groupInfoService) SetGroupsStatus(uuidList []string, status int8) error {
	// 管理员禁用群时，除了改状态，还会把和这个群有关的会话软删除。
	var deletedAt gorm.DeletedAt
	deletedAt.Time = time.Now()
	deletedAt.Valid = true
	for _, uuid := range uuidList {
		if res := dao.GormDB.Model(&model.GroupInfo{}).Where("uuid = ?", uuid).Update("status", status); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		if status == group_status_enum.DISABLE {
			var sessionList []model.Session
			if res := dao.GormDB.Model(&sessionList).Where("receive_id = ?", uuid).Find(&sessionList); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
			for _, session := range sessionList {
				if res := dao.GormDB.Model(&session).Update("deleted_at", deletedAt); res.Error != nil {
					zlog.Error(res.Error.Error())
					return apperr.SystemError(res.Error)
				}
			}
		}
	}
	return nil
}

// UpdateGroupInfo 更新群聊消息
func (g *groupInfoService) UpdateGroupInfo(req request.UpdateGroupInfoRequest) error {
	// 先改群本身的资料字段。
	var group model.GroupInfo
	if res := dao.GormDB.First(&group, "uuid = ?", req.Uuid); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if req.Name != "" {
		group.Name = req.Name
	}
	if req.AddMode != -1 {
		group.AddMode = req.AddMode
	}
	if req.Notice != "" {
		group.Notice = req.Notice
	}
	if req.Avatar != "" {
		group.Avatar = req.Avatar
	}
	if res := dao.GormDB.Save(&group); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	// 再把引用了群名称、头像的会话记录一并同步。
	var sessionList []model.Session
	if res := dao.GormDB.Where("receive_id = ?", req.Uuid).Find(&sessionList); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	for _, session := range sessionList {
		session.ReceiveName = group.Name
		session.Avatar = group.Avatar
		log.Println(session)
		if res := dao.GormDB.Save(&session); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}

	// Cache-Aside：群资料（名称/头像）变更后，失效群详情缓存与各成员的群会话列表缓存，
	// 避免成员侧仍显示旧群名/旧头像。
	if err := myredis.DelKeysWithPattern("group_info_" + req.Uuid); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("group_session_list_"); err != nil {
		zlog.Error(err.Error())
	}
	return nil
}

// GetGroupMemberList 获取群聊成员列表
func (g *groupInfoService) GetGroupMemberList(groupId string) ([]respond.GetGroupMemberListRespond, error) {
	// 当前成员列表的真实来源仍然是 group_info.members 这段 JSON。
	rspString, err := myredis.GetKeyNilIsErr("group_memberlist_" + groupId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var group model.GroupInfo
			if res := dao.GormDB.First(&group, "uuid = ?", groupId); res.Error != nil {
				zlog.Error(res.Error.Error())
				return nil, apperr.SystemError(res.Error)
			}
			var members []string
			if err := json.Unmarshal(group.Members, &members); err != nil {
				zlog.Error(err.Error())
				return nil, apperr.SystemError(err)
			}
			var rspList []respond.GetGroupMemberListRespond
			for _, member := range members {
				var user model.UserInfo
				if res := dao.GormDB.First(&user, "uuid = ?", member); res.Error != nil {
					zlog.Error(res.Error.Error())
					return nil, apperr.SystemError(res.Error)
				}
				rspList = append(rspList, respond.GetGroupMemberListRespond{
					UserId:   user.Uuid,
					Nickname: user.Nickname,
					Avatar:   user.Avatar,
				})
			}
			if rspList == nil {
				rspList = []respond.GetGroupMemberListRespond{}
			}
			return rspList, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp []respond.GetGroupMemberListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return rsp, nil
}

// RemoveGroupMembers 移除群聊成员
func (g *groupInfoService) RemoveGroupMembers(req request.RemoveGroupMembersRequest) error {
	// 批量踢人时，需要同时处理群成员、联系人、会话、申请记录。
	var group model.GroupInfo
	if res := dao.GormDB.First(&group, "uuid = ?", req.GroupId); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	var members []string
	if err := json.Unmarshal(group.Members, &members); err != nil {
		zlog.Error(err.Error())
		return apperr.SystemError(err)
	}
	var deletedAt gorm.DeletedAt
	deletedAt.Time = time.Now()
	deletedAt.Valid = true
	log.Println(req.UuidList, req.OwnerId)
	for _, uuid := range req.UuidList {
		if req.OwnerId == uuid {
			return apperr.Biz("不能移除群主")
		}
		// 从members中找到uuid，移除
		for i, member := range members {
			if member == uuid {
				members = append(members[:i], members[i+1:]...)
			}
		}
		group.MemberCnt -= 1
		// 删除会话
		if res := dao.GormDB.Model(&model.Session{}).Where("send_id = ? AND receive_id = ?", uuid, req.GroupId).Update("deleted_at", deletedAt); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		// 删除联系人
		if res := dao.GormDB.Model(&model.UserContact{}).Where("user_id = ? AND contact_id = ?", uuid, req.GroupId).Update("deleted_at", deletedAt); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		// 删除申请记录
		if res := dao.GormDB.Model(&model.ContactApply{}).Where("user_id = ? AND contact_id = ?", uuid, req.GroupId).Update("deleted_at", deletedAt); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}
	group.Members, _ = json.Marshal(members)
	if res := dao.GormDB.Save(&group); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}

	// 群成员变化后，和群有关的会话列表、已加入群列表都要失效。
	if err := myredis.DelKeysWithPrefix("group_session_list"); err != nil {
		zlog.Error(err.Error())
	}
	invalidateAllMemberJoinedGroupCache()
	return nil
}
