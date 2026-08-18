package gorm

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gochat/internal/config"
	"gochat/internal/dao"
	"gochat/internal/dto/respond"
	"gochat/internal/model"
	myredis "gochat/internal/service/redis"
	"gochat/pkg/apperr"
	"gochat/pkg/constants"
	"gochat/pkg/zlog"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gochat/pkg/util/random"
)

type messageService struct {
}

var MessageService = new(messageService)

// GetMessageList 获取聊天记录
func (m *messageService) GetMessageList(userOneId, userTwoId string) ([]respond.GetMessageListRespond, error) {
	rspString, err := myredis.GetKeyNilIsErr("message_list_" + userOneId + "_" + userTwoId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			zlog.Info(err.Error())
			zlog.Info(fmt.Sprintf("%s %s", userTwoId, userTwoId))
			var messageList []model.Message
			if res := dao.GormDB.Where("(send_id = ? AND receive_id = ?) OR (send_id = ? AND receive_id = ?)", userOneId, userTwoId, userTwoId, userOneId).Order("created_at ASC").Find(&messageList); res.Error != nil {
				zlog.Error(res.Error.Error())
				return nil, apperr.SystemError(res.Error)
			}
			var rspList []respond.GetMessageListRespond
			for _, message := range messageList {
				rspList = append(rspList, respond.GetMessageListRespond{
					SendId:     message.SendId,
					SendName:   message.SendName,
					SendAvatar: message.SendAvatar,
					ReceiveId:  message.ReceiveId,
					Content:    message.Content,
					Url:        message.Url,
					Type:       message.Type,
					FileType:   message.FileType,
					FileName:   message.FileName,
					FileSize:   message.FileSize,
					CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
				})
			}
			if rspList == nil {
				rspList = []respond.GetMessageListRespond{}
			}
			return rspList, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp []respond.GetMessageListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return rsp, nil
}

// GetGroupMessageList 获取群聊消息记录
func (m *messageService) GetGroupMessageList(groupId string) ([]respond.GetGroupMessageListRespond, error) {
	rspString, err := myredis.GetKeyNilIsErr("group_messagelist_" + groupId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			var messageList []model.Message
			if res := dao.GormDB.Where("receive_id = ?", groupId).Order("created_at ASC").Find(&messageList); res.Error != nil {
				zlog.Error(res.Error.Error())
				return nil, apperr.SystemError(res.Error)
			}
			var rspList []respond.GetGroupMessageListRespond
			for _, message := range messageList {
				rsp := respond.GetGroupMessageListRespond{
					SendId:     message.SendId,
					SendName:   message.SendName,
					SendAvatar: message.SendAvatar,
					ReceiveId:  message.ReceiveId,
					Content:    message.Content,
					Url:        message.Url,
					Type:       message.Type,
					FileType:   message.FileType,
					FileName:   message.FileName,
					FileSize:   message.FileSize,
					CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
				}
				rspList = append(rspList, rsp)
			}
			if rspList == nil {
				rspList = []respond.GetGroupMessageListRespond{}
			}
			return rspList, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp []respond.GetGroupMessageListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return rsp, nil
}

// allowedAvatarExts 头像允许的图片扩展名（小写）。
var allowedAvatarExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
}

// allowedFileExts 聊天文件允许的扩展名（小写）。
// 只放行文档/图片/压缩包/音视频等可安全共享的类型，
// 排除 .html/.htm/.svg/.js/.php/.sh 等可执行或可被浏览器直接运行的脚本类，
// 防止上传的"文件"被当作静态资源直接执行（XSS / 挂马风险）。
var allowedFileExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".txt": true, ".md": true,
	".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
	".mp3": true, ".wav": true, ".mp4": true, ".avi": true, ".mov": true, ".mkv": true,
	".apk": true,
}

// saveUploadedFile 把 multipart 表单中的文件落盘到 dir，返回可访问的相对路径。
//   - 文件名只取 base（filepath.Base 去掉路径部分，防目录穿越）；
//   - 扩展名必须在白名单内（头像/文件分别校验）；
//   - 落盘文件名改为"时间戳+随机串+原扩展名"，避免同名文件互相覆盖。
// 返回的相对路径格式：/static/<dir>/<newName>。
func saveUploadedFile(c *gin.Context, dir string, allowed map[string]bool) (string, error) {
	if err := c.Request.ParseMultipartForm(constants.FILE_MAX_SIZE); err != nil {
		zlog.Error(err.Error())
		return "", apperr.SystemError(err)
	}
	mForm := c.Request.MultipartForm
	for key := range mForm.File {
		file, fileHeader, err := c.Request.FormFile(key)
		if err != nil {
			zlog.Error(err.Error())
			return "", apperr.SystemError(err)
		}
		defer file.Close()
		zlog.Info(fmt.Sprintf("文件名：%s，文件大小：%d", fileHeader.Filename, fileHeader.Size))

		// 1. 只取文件名本身，拒绝路径穿越（../、绝对路径等）
		baseName := filepath.Base(fileHeader.Filename)
		if baseName == "." || baseName == "/" || baseName == "" {
			return "", apperr.Biz("文件名不合法")
		}
		// 2. 扩展名白名单
		ext := strings.ToLower(filepath.Ext(baseName))
		if !allowed[ext] {
			return "", apperr.Biz("不支持的文件类型：" + ext)
		}
		// 3. 随机新文件名：<毫秒时间戳>_<6位随机>.<ext>，避免同名覆盖
		newName := fmt.Sprintf("%d_%s%s", time.Now().UnixMilli(), random.GetNowAndLenRandomString(6), ext)
		localFileName := filepath.Join(dir, newName)
		out, err := os.Create(localFileName)
		if err != nil {
			zlog.Error(err.Error())
			return "", apperr.SystemError(err)
		}
		defer out.Close()
		if _, err := io.Copy(out, file); err != nil {
			zlog.Error(err.Error())
			return "", apperr.SystemError(err)
		}
		zlog.Info("完成文件上传：" + localFileName)
		// 只处理第一个文件（前端单文件上传），避免同名 key 循环覆盖
		return "/static/" + dirBase(dir) + "/" + newName, nil
	}
	return "", apperr.Biz("未收到上传文件")
}

// dirBase 去掉目录尾分隔符后取最后一段，供拼相对 URL 用。
func dirBase(dir string) string {
	dir = strings.TrimRight(dir, `/\`)
	return filepath.Base(dir)
}

// UploadAvatar 上传头像
func (m *messageService) UploadAvatar(c *gin.Context) (string, error) {
	return saveUploadedFile(c, config.GetConfig().StaticSrcConfig.StaticAvatarPath, allowedAvatarExts)
}

// UploadFile 上传文件
func (m *messageService) UploadFile(c *gin.Context) (string, error) {
	return saveUploadedFile(c, config.GetConfig().StaticSrcConfig.StaticFilePath, allowedFileExts)
}
