package gorm

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"gochat/internal/dao"
	"gochat/internal/dto/request"
	"gochat/internal/dto/respond"
	"gochat/internal/model"
	myredis "gochat/internal/service/redis"
	"gochat/pkg/apperr"
	"gochat/pkg/constants"
	"gochat/pkg/enum/contact/contact_status_enum"
	"gochat/pkg/enum/group_info/group_status_enum"
	"gochat/pkg/enum/message/message_type_enum"
	"gochat/pkg/enum/user_info/user_status_enum"
	"gochat/pkg/util/random"
	"gochat/pkg/zlog"
	"gorm.io/gorm"
)

type sessionService struct {
}

var SessionService = new(sessionService)

// CreateSession 创建会话
func (s *sessionService) CreateSession(req request.CreateSessionRequest) (string, error) {
	var user model.UserInfo
	if res := dao.GormDB.Where("uuid = ?", req.SendId).First(&user); res.Error != nil {
		zlog.Error(res.Error.Error())
		return "", apperr.SystemError(res.Error)
	}
	var session model.Session
	session.Uuid = fmt.Sprintf("S%s", random.GetNowAndLenRandomString(11))
	session.SendId = req.SendId
	session.ReceiveId = req.ReceiveId
	session.CreatedAt = time.Now()
	if req.ReceiveId[0] == 'U' {
		var receiveUser model.UserInfo
		if res := dao.GormDB.Where("uuid = ?", req.ReceiveId).First(&receiveUser); res.Error != nil {
			zlog.Error(res.Error.Error())
			return "", apperr.SystemError(res.Error)
		}
		if receiveUser.Status == user_status_enum.DISABLE {
			zlog.Error("该用户被禁用了")
			return "", apperr.Biz("该用户被禁用了")
		} else {
			session.ReceiveName = receiveUser.Nickname
			session.Avatar = receiveUser.Avatar
		}
	} else {
		var receiveGroup model.GroupInfo
		if res := dao.GormDB.Where("uuid = ?", req.ReceiveId).First(&receiveGroup); res.Error != nil {
			zlog.Error(res.Error.Error())
			return "", apperr.SystemError(res.Error)
		}
		if receiveGroup.Status == group_status_enum.DISABLE {
			zlog.Error("该群聊被禁用了")
			return "", apperr.Biz("该群聊被禁用了")
		} else {
			session.ReceiveName = receiveGroup.Name
			session.Avatar = receiveGroup.Avatar
		}
	}

	if res := dao.GormDB.Create(&session); res.Error != nil {
		zlog.Error(res.Error.Error())
		return "", apperr.SystemError(res.Error)
	}
	if err := myredis.DelKeysWithPattern("group_session_list_" + req.SendId); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("session_list_" + req.SendId); err != nil {
		zlog.Error(err.Error())
	}
	return session.Uuid, nil
}

// CheckOpenSessionAllowed 检查是否允许发起会话
func (s *sessionService) CheckOpenSessionAllowed(sendId, receiveId string) (bool, error) {
	var contact model.UserContact
	if res := dao.GormDB.Where("user_id = ? and contact_id = ?", sendId, receiveId).First(&contact); res.Error != nil {
		zlog.Error(res.Error.Error())
		return false, apperr.SystemError(res.Error)
	}
	if contact.Status == contact_status_enum.BE_BLACK {
		return false, apperr.Biz("已被对方拉黑，无法发起会话")
	} else if contact.Status == contact_status_enum.BLACK {
		return false, apperr.Biz("已拉黑对方，先解除拉黑状态才能发起会话")
	}
	if receiveId[0] == 'U' {
		var user model.UserInfo
		if res := dao.GormDB.Where("uuid = ?", receiveId).First(&user); res.Error != nil {
			zlog.Error(res.Error.Error())
			return false, apperr.SystemError(res.Error)
		}
		if user.Status == user_status_enum.DISABLE {
			zlog.Info("对方已被禁用，无法发起会话")
			return false, apperr.Biz("对方已被禁用，无法发起会话")
		}
	} else {
		var group model.GroupInfo
		if res := dao.GormDB.Where("uuid = ?", receiveId).First(&group); res.Error != nil {
			zlog.Error(res.Error.Error())
			return false, apperr.SystemError(res.Error)
		}
		if group.Status == group_status_enum.DISABLE {
			zlog.Info("对方已被禁用，无法发起会话")
			return false, apperr.Biz("对方已被禁用，无法发起会话")
		}
	}
	return true, nil
}

// OpenSession 打开会话：查库（双向：send_id/receive_id 或反向均视为同一对与会话），
// 不存在则创建。避免同一对用户因先后 openSession 建出两条镜像会话（各自侧栏各一条）。
// （历史实现有一段"缓存查找"是死代码：回填从未启用，且查找返回的是 key 名而非值，
//
//	一旦启用会解出空对象；已整体移除，每次调用一次 DB 查询即可。）
func (s *sessionService) OpenSession(req request.OpenSessionRequest) (string, error) {
	var session model.Session
	if res := dao.GormDB.Where("send_id = ? AND receive_id = ?", req.SendId, req.ReceiveId).
		Or("send_id = ? AND receive_id = ?", req.ReceiveId, req.SendId).
		First(&session); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			zlog.Info("会话没有找到，将新建会话")
			createReq := request.CreateSessionRequest{
				SendId:    req.SendId,
				ReceiveId: req.ReceiveId,
			}
			return s.CreateSession(createReq)
		}
		zlog.Error(res.Error.Error())
		return "", apperr.SystemError(res.Error)
	}
	return session.Uuid, nil
}

// latestSessionMessages 查一批会话各自最近一条消息(session_id 有索引)。
// 按 id 倒序扫、每个会话只留首条,拿齐即停;查询失败不阻塞列表,只是没有预览。
func latestSessionMessages(sessionIds []string) map[string]model.Message {
	latest := make(map[string]model.Message, len(sessionIds))
	if len(sessionIds) == 0 {
		return latest
	}
	var msgs []model.Message
	if res := dao.GormDB.
		Select("id", "session_id", "type", "content", "file_type", "file_name", "send_id", "send_name").
		Where("session_id IN ?", sessionIds).
		Order("id DESC").
		Find(&msgs); res.Error != nil {
		zlog.Error(res.Error.Error())
		return latest
	}
	for _, m := range msgs {
		if _, ok := latest[m.SessionId]; !ok {
			latest[m.SessionId] = m
		}
		if len(latest) == len(sessionIds) {
			break
		}
	}
	return latest
}

// sessionPreviewText 与前端实时预览的规则保持一致:文件按 MIME 折叠成占位文案,
// 群聊里非本人发送的消息带“发送者：”前缀。
func sessionPreviewText(msg model.Message, ownerId string, isGroup bool) string {
	var text string
	switch msg.Type {
	case message_type_enum.File:
		switch {
		case strings.HasPrefix(msg.FileType, "image/"):
			text = "[图片]"
		case strings.HasPrefix(msg.FileType, "video/"):
			text = "[视频]"
		case strings.HasPrefix(msg.FileType, "audio/"):
			text = "[语音]"
		default:
			text = "[文件] " + msg.FileName
		}
	default:
		text = msg.Content
	}
	if isGroup && msg.SendId != ownerId {
		text = msg.SendName + "：" + text
	}
	return text
}

// fillUserSessionPreviews 回填单聊会话的最近消息预览,并按最近消息重排
// (无消息的会话保持 created_at 倒序垫底)。预览不进缓存:缓存命中路径同样现查,
// 新消息后下一次拉取即可见,无需失效缓存。
func fillUserSessionPreviews(list []respond.UserSessionListRespond, ownerId string) {
	ids := make([]string, 0, len(list))
	for i := range list {
		ids = append(ids, list[i].SessionId)
	}
	latest := latestSessionMessages(ids)
	rank := make(map[string]int64, len(list))
	for i := range list {
		if msg, ok := latest[list[i].SessionId]; ok {
			list[i].LastMessage = sessionPreviewText(msg, ownerId, false)
			rank[list[i].SessionId] = msg.Id
		}
	}
	sort.SliceStable(list, func(a, b int) bool {
		return rank[list[a].SessionId] > rank[list[b].SessionId]
	})
}

// fillGroupSessionPreviews 同上,群聊版(非本人消息带发送者前缀)。
func fillGroupSessionPreviews(list []respond.GroupSessionListRespond, ownerId string) {
	ids := make([]string, 0, len(list))
	for i := range list {
		ids = append(ids, list[i].SessionId)
	}
	latest := latestSessionMessages(ids)
	rank := make(map[string]int64, len(list))
	for i := range list {
		if msg, ok := latest[list[i].SessionId]; ok {
			list[i].LastMessage = sessionPreviewText(msg, ownerId, true)
			rank[list[i].SessionId] = msg.Id
		}
	}
	sort.SliceStable(list, func(a, b int) bool {
		return rank[list[a].SessionId] > rank[list[b].SessionId]
	})
}

// GetUserSessionList 获取用户会话列表（双向：send_id / receive_id 任一端命中即展示，
// 与 OpenSession 的双向查找配套，避免"复用对方方向会话后自己的列表看不到"）。
// 会话行里 ReceiveName/Avatar 是接收端视角的信息，反向命中时对端是 SendId，
// 需回查 user_info 补全昵称头像。
func (s *sessionService) GetUserSessionList(ownerId string) ([]respond.UserSessionListRespond, error) {
	rspString, err := myredis.GetKeyNilIsErr("session_list_" + ownerId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var sessionList []model.Session
			if res := dao.GormDB.Order("created_at DESC").Where("send_id = ?", ownerId).Or("receive_id = ?", ownerId).Find(&sessionList); res.Error != nil {
				if errors.Is(res.Error, gorm.ErrRecordNotFound) {
					zlog.Info("未创建用户会话")
					return []respond.UserSessionListRespond{}, nil
				} else {
					zlog.Error(res.Error.Error())
					return nil, apperr.SystemError(res.Error)
				}
			}
			var sessionListRsp []respond.UserSessionListRespond
			for i := 0; i < len(sessionList); i++ {
				if sessionList[i].ReceiveId[0] == 'U' {
					// 双向命中的会话：对端 = 非 ownerId 的一端。
					peerId := sessionList[i].ReceiveId
					peerName := sessionList[i].ReceiveName
					peerAvatar := sessionList[i].Avatar
					if sessionList[i].SendId == ownerId {
						// 自己发起的，receive 端即对端
						sessionListRsp = append(sessionListRsp, respond.UserSessionListRespond{
							SessionId: sessionList[i].Uuid,
							Avatar:    peerAvatar,
							UserId:    peerId,
							Username:  peerName,
						})
					} else {
						// 对方发起的反向会话：receive 端是自己，对端是 send 端，回查 user_info
						var peer model.UserInfo
						if res := dao.GormDB.Select("uuid", "nickname", "avatar").Where("uuid = ?", sessionList[i].SendId).First(&peer); res.Error != nil {
							zlog.Error(res.Error.Error())
							continue
						}
						sessionListRsp = append(sessionListRsp, respond.UserSessionListRespond{
							SessionId: sessionList[i].Uuid,
							Avatar:    peer.Avatar,
							UserId:    peer.Uuid,
							Username:  peer.Nickname,
						})
					}
				}
			}
			if sessionListRsp == nil {
				sessionListRsp = []respond.UserSessionListRespond{}
			}
			rspString, err := json.Marshal(sessionListRsp)
			if err != nil {
				zlog.Error(err.Error())
			}
			if err := myredis.SetKeyExJitter("session_list_"+ownerId, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
				zlog.Error(err.Error())
			}
			fillUserSessionPreviews(sessionListRsp, ownerId)
			return sessionListRsp, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp []respond.UserSessionListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	fillUserSessionPreviews(rsp, ownerId)
	return rsp, nil
}

// GetGroupSessionList 获取群聊会话列表
func (s *sessionService) GetGroupSessionList(ownerId string) ([]respond.GroupSessionListRespond, error) {
	rspString, err := myredis.GetKeyNilIsErr("group_session_list_" + ownerId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var sessionList []model.Session
			if res := dao.GormDB.Order("created_at DESC").Where("send_id = ?", ownerId).Find(&sessionList); res.Error != nil {
				if errors.Is(res.Error, gorm.ErrRecordNotFound) {
					zlog.Info("未创建群聊会话")
					return []respond.GroupSessionListRespond{}, nil
				} else {
					zlog.Error(res.Error.Error())
					return nil, apperr.SystemError(res.Error)
				}
			}
			var sessionListRsp []respond.GroupSessionListRespond
			for i := 0; i < len(sessionList); i++ {
				if sessionList[i].ReceiveId[0] == 'G' {
					sessionListRsp = append(sessionListRsp, respond.GroupSessionListRespond{
						SessionId: sessionList[i].Uuid,
						Avatar:    sessionList[i].Avatar,
						GroupId:   sessionList[i].ReceiveId,
						GroupName: sessionList[i].ReceiveName,
					})
				}
			}
			if sessionListRsp == nil {
				sessionListRsp = []respond.GroupSessionListRespond{}
			}
			rspString, err := json.Marshal(sessionListRsp)
			if err != nil {
				zlog.Error(err.Error())
			}
			if err := myredis.SetKeyExJitter("group_session_list_"+ownerId, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
				zlog.Error(err.Error())
			}
			fillGroupSessionPreviews(sessionListRsp, ownerId)
			return sessionListRsp, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp []respond.GroupSessionListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	fillGroupSessionPreviews(rsp, ownerId)
	return rsp, nil
}

// DeleteSession 删除会话（幂等：session 不存在也返回成功），并清理会话两端用户的列表缓存。
func (s *sessionService) DeleteSession(ownerId, sessionId string) error {
	var session model.Session
	if res := dao.GormDB.Where("uuid = ?", sessionId).Find(&session); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	session.DeletedAt.Valid = true
	session.DeletedAt.Time = time.Now()
	if res := dao.GormDB.Save(&session); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	// 双向命中：两端用户的会话列表缓存都要失效。
	affectedIds := []string{ownerId}
	if session.SendId != "" && session.SendId != ownerId {
		affectedIds = append(affectedIds, session.SendId)
	}
	if session.ReceiveId != "" && session.ReceiveId != ownerId && session.ReceiveId != session.SendId {
		affectedIds = append(affectedIds, session.ReceiveId)
	}
	for _, id := range affectedIds {
		if err := myredis.DelKeysWithPattern("group_session_list_" + id); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("session_list_" + id); err != nil {
			zlog.Error(err.Error())
		}
	}
	return nil
}
