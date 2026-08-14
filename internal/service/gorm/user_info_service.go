package gorm

import (
	"encoding/json"
	"errors"
	"fmt"
	"gochat/internal/dao"
	"gochat/internal/dto/request"
	"gochat/internal/dto/respond"
	"gochat/internal/model"
	"gochat/internal/service/auth"
	"gochat/internal/service/chat"
	myredis "gochat/internal/service/redis"
	"gochat/internal/service/sms"
	"gochat/pkg/apperr"
	"gochat/pkg/enum/user_info/user_status_enum"
	"gochat/pkg/util/random"
	"gochat/pkg/zlog"
	"go.uber.org/zap"
	"regexp"
	"strconv"
	"strings"
	"time"

	redis "github.com/go-redis/redis/v8"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type userInfoService struct {
}

var UserInfoService = new(userInfoService)

// dao层加不了校验，在service层加
// checkTelephoneValid 检验电话是否有效
func (u *userInfoService) checkTelephoneValid(telephone string) bool {
	pattern := `^1([38][0-9]|14[579]|5[^4]|16[6]|7[1-35-8]|9[189])\d{8}$`
	match, err := regexp.MatchString(pattern, telephone)
	if err != nil {
		zlog.Error(err.Error())
	}
	return match
}

// checkEmailValid 校验邮箱是否有效
func (u *userInfoService) checkEmailValid(email string) bool {
	pattern := `^[^\s@]+@[^\s@]+\.[^\s@]+$`
	match, err := regexp.MatchString(pattern, email)
	if err != nil {
		zlog.Error(err.Error())
	}
	return match
}

// checkUserIsAdminOrNot 检验用户是否为管理员
func (u *userInfoService) checkUserIsAdminOrNot(user model.UserInfo) int8 {
	return user.IsAdmin
}

// Login 登录（密码）。成功时更新 last_online_at，并清除该手机号的失败计数。
func (u *userInfoService) Login(loginReq request.LoginRequest) (*respond.LoginRespond, error) {
	password := loginReq.Password
	var user model.UserInfo
	res := dao.GormDB.First(&user, "telephone = ?", loginReq.Telephone)
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			message := "用户不存在，请注册"
			zlog.Error(message)
			return nil, apperr.Biz(message)
		}
		zlog.Error(res.Error.Error())
		return nil, apperr.SystemError(res.Error)
	}
	if user.Status == user_status_enum.DISABLE {
		message := "该账号已被禁用，请联系管理员"
		zlog.Error(message)
		return nil, apperr.Biz(message)
	}
	if err := u.checkPassword(&user, password); err != nil {
		// 登录失败按手机号分钟级限流，防撞库。
		u.recordLoginFailure(loginReq.Telephone)
		return nil, err
	}
	u.clearLoginFailure(loginReq.Telephone)

	// 存量明文密码透明升级：登录成功后立即重哈希回写。
	if !strings.HasPrefix(user.Password, "$2") {
		if hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost); hashErr == nil {
			if err := dao.GormDB.Model(&model.UserInfo{}).Where("uuid = ?", user.Uuid).Update("password", string(hash)).Error; err != nil {
				zlog.Error(err.Error())
			}
		}
	}

	loginRsp := &respond.LoginRespond{
		Uuid:      user.Uuid,
		Telephone: user.Telephone,
		Nickname:  user.Nickname,
		Email:     user.Email,
		Avatar:    user.Avatar,
		Gender:    user.Gender,
		Birthday:  user.Birthday,
		Signature: user.Signature,
		IsAdmin:   user.IsAdmin,
		Status:    user.Status,
	}
	year, month, day := user.CreatedAt.Date()
	loginRsp.CreatedAt = fmt.Sprintf("%d.%d.%d", year, month, day)

	u.markOnline(user.Uuid)
	return loginRsp, nil
}

// checkPassword 兼容 bcrypt 哈希与存量明文密码。
func (u *userInfoService) checkPassword(user *model.UserInfo, password string) error {
	if strings.HasPrefix(user.Password, "$2") {
		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
			zlog.Error("密码不正确，请重试")
			return apperr.Biz("密码不正确，请重试")
		}
		return nil
	}
	// 存量明文（迁移前注册的用户）：直接比较，由 Login 负责懒升级。
	if user.Password != password {
		zlog.Error("密码不正确，请重试")
		return apperr.Biz("密码不正确，请重试")
	}
	return nil
}

// CheckLoginRateLimit 检查手机号是否处于登录失败退避期（连续失败 >= 5 次）。
func (u *userInfoService) CheckLoginRateLimit(telephone string) error {
	value, err := myredis.GetKey("login_fail_" + telephone)
	if err != nil {
		zlog.Error(err.Error())
		return nil
	}
	if value == "" {
		return nil
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	if count >= 5 {
		return apperr.Biz("登录失败次数过多，请 1 分钟后再试")
	}
	return nil
}

// recordLoginFailure 记录一次登录失败；达到阈值后按分钟级退避（由调用方在失败路径调用）。
func (u *userInfoService) recordLoginFailure(telephone string) {
	key := "login_fail_" + telephone
	count, err := myredis.Incr(key)
	if err != nil {
		zlog.Error(err.Error())
		return
	}
	if count == 1 {
		_ = myredis.Expire(key, time.Minute)
	}
	if count >= 5 {
		zlog.Warn("登录失败次数过多，触发限流", zap.String("telephone", telephone))
	}
}

// clearLoginFailure 登录成功后清除失败计数。
func (u *userInfoService) clearLoginFailure(telephone string) {
	if err := myredis.DelKeys("login_fail_" + telephone); err != nil {
		zlog.Error(err.Error())
	}
}

// markOnline 更新最近在线时间。
func (u *userInfoService) markOnline(uuid string) {
	if err := dao.GormDB.Model(&model.UserInfo{}).Where("uuid = ?", uuid).Update("last_online_at", time.Now()).Error; err != nil {
		zlog.Error(err.Error())
	}
}

// SmsLogin 验证码登录
func (u *userInfoService) SmsLogin(req request.SmsLoginRequest) (*respond.LoginRespond, error) {
	var user model.UserInfo
	res := dao.GormDB.First(&user, "telephone = ?", req.Telephone)
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			message := "用户不存在，请注册"
			zlog.Error(message)
			return nil, apperr.Biz(message)
		}
		zlog.Error(res.Error.Error())
		return nil, apperr.SystemError(res.Error)
	}
	if user.Status == user_status_enum.DISABLE {
		message := "该账号已被禁用，请联系管理员"
		zlog.Error(message)
		return nil, apperr.Biz(message)
	}

	if err := sms.CheckVerificationCode(req.Telephone, req.SmsCode); err != nil {
		return nil, err
	}

	loginRsp := &respond.LoginRespond{
		Uuid:      user.Uuid,
		Telephone: user.Telephone,
		Nickname:  user.Nickname,
		Email:     user.Email,
		Avatar:    user.Avatar,
		Gender:    user.Gender,
		Birthday:  user.Birthday,
		Signature: user.Signature,
		IsAdmin:   user.IsAdmin,
		Status:    user.Status,
	}
	year, month, day := user.CreatedAt.Date()
	loginRsp.CreatedAt = fmt.Sprintf("%d.%d.%d", year, month, day)

	u.markOnline(user.Uuid)
	return loginRsp, nil
}

// SendSmsCode 发送短信验证码 - 验证码登录
func (u *userInfoService) SendSmsCode(telephone string) error {
	return sms.SendVerificationCode(telephone)
}

// checkTelephoneExist 检查手机号是否存在
func (u *userInfoService) checkTelephoneExist(telephone string) error {
	var user model.UserInfo
	// gorm默认排除软删除，所以翻译过来的select语句是SELECT * FROM `user_info` WHERE telephone = '18089596095' AND `user_info`.`deleted_at` IS NULL ORDER BY `user_info`.`id` LIMIT 1
	if res := dao.GormDB.Where("telephone = ?", telephone).First(&user); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			zlog.Info("该电话不存在，可以注册")
			return nil
		}
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	zlog.Info("该电话已经存在，注册失败")
	return apperr.Biz("该电话已经存在，注册失败")
}

// Register 注册
func (u *userInfoService) Register(registerReq request.RegisterRequest) (*respond.LoginRespond, error) {
	// 校验验证码
	if err := sms.CheckVerificationCode(registerReq.Telephone, registerReq.SmsCode); err != nil {
		return nil, err
	}
	// 不用校验手机号，前端校验
	// 判断电话是否已经被注册过了
	if err := u.checkTelephoneExist(registerReq.Telephone); err != nil {
		return nil, err
	}
	var newUser model.UserInfo
	// 硬编码前缀 TODO
	newUser.Uuid = "U" + random.GetNowAndLenRandomString(11)
	newUser.Telephone = registerReq.Telephone
	// 密码只存 bcrypt 哈希，不存明文。
	hash, err := bcrypt.GenerateFromPassword([]byte(registerReq.Password), bcrypt.DefaultCost)
	if err != nil {
		zlog.Error(err.Error())
		return nil, apperr.SystemError(err)
	}
	newUser.Password = string(hash)
	newUser.Nickname = registerReq.Nickname
	// 硬编码默认头像  TODO
	newUser.Avatar = "https://cube.elemecdn.com/0/88/03b0d39583f48206768a7534e55bcpng.png"
	newUser.CreatedAt = time.Now()
	newUser.IsAdmin = u.checkUserIsAdminOrNot(newUser)
	newUser.Status = user_status_enum.NORMAL
	// 手机号验证，最后一步才调用api，省钱hhh
	//err := sms.VerificationCode(registerReq.Telephone)
	//if err != nil {
	//	zlog.Error(err.Error())
	//	return "", err
	//}

	res := dao.GormDB.Create(&newUser)
	if res.Error != nil {
		zlog.Error(res.Error.Error())
		return nil, apperr.SystemError(res.Error)
	}
	registerRsp := &respond.LoginRespond{
		Uuid:      newUser.Uuid,
		Telephone: newUser.Telephone,
		Nickname:  newUser.Nickname,
		Email:     newUser.Email,
		Avatar:    newUser.Avatar,
		Gender:    newUser.Gender,
		Birthday:  newUser.Birthday,
		Signature: newUser.Signature,
		IsAdmin:   newUser.IsAdmin,
		Status:    newUser.Status,
	}
	year, month, day := newUser.CreatedAt.Date()
	registerRsp.CreatedAt = fmt.Sprintf("%d.%d.%d", year, month, day)

	u.markOnline(newUser.Uuid)
	return registerRsp, nil
}

// UpdateUserInfo 修改用户信息
// 某用户修改了信息，可能会影响contact_user_list，不需要删除redis的contact_user_list，timeout之后会自己更新
// 但是需要更新redis的user_info，因为可能影响用户搜索
func (u *userInfoService) UpdateUserInfo(updateReq request.UpdateUserInfoRequest) error {
	var user model.UserInfo
	if res := dao.GormDB.First(&user, "uuid = ?", updateReq.Uuid); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	if updateReq.Email != "" {
		user.Email = updateReq.Email
	}
	if updateReq.Nickname != "" {
		user.Nickname = updateReq.Nickname
	}
	if updateReq.Birthday != "" {
		user.Birthday = updateReq.Birthday
	}
	if updateReq.Signature != "" {
		user.Signature = updateReq.Signature
	}
	if updateReq.Avatar != "" {
		user.Avatar = updateReq.Avatar
	}
	if res := dao.GormDB.Save(&user); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	return nil
}

// GetUserInfoList 获取用户列表除了ownerId之外 - 管理员
// 管理员少，而且如果用户更改了，那么管理员会一直频繁删除redis，更新redis，比较麻烦，所以管理员暂时不使用redis缓存
func (u *userInfoService) GetUserInfoList(ownerId string) ([]respond.GetUserListRespond, error) {
	// redis中没有数据，从数据库中获取
	var users []model.UserInfo
	// 获取所有的用户
	if res := dao.GormDB.Unscoped().Where("uuid != ?", ownerId).Find(&users); res.Error != nil {
		zlog.Error(res.Error.Error())
		return nil, apperr.SystemError(res.Error)
	}
	var rsp []respond.GetUserListRespond
	for _, user := range users {
		rp := respond.GetUserListRespond{
			Uuid:      user.Uuid,
			Telephone: user.Telephone,
			Nickname:  user.Nickname,
			Status:    user.Status,
			IsAdmin:   user.IsAdmin,
		}
		if user.DeletedAt.Valid {
			rp.IsDeleted = true
		} else {
			rp.IsDeleted = false
		}
		rsp = append(rsp, rp)
	}
	if rsp == nil {
		rsp = []respond.GetUserListRespond{}
	}
	return rsp, nil
}

// AbleUsers 启用用户
// 用户是否启用禁用需要实时更新contact_user_list状态，所以redis的contact_user_list需要删除
func (u *userInfoService) AbleUsers(uuidList []string) error {
	var users []model.UserInfo
	if res := dao.GormDB.Model(model.UserInfo{}).Where("uuid in (?)", uuidList).Find(&users); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	for _, user := range users {
		user.Status = user_status_enum.NORMAL
		if res := dao.GormDB.Save(&user); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}
	// 删除所有"contact_user_list"开头的key
	//if err := myredis.DelKeysWithPrefix("contact_user_list"); err != nil {
	//	zlog.Error(err.Error())
	//}
	return nil
}

// DisableUsers 禁用用户
// 用户是否启用禁用需要实时更新contact_user_list状态，所以redis的contact_user_list需要删除
func (u *userInfoService) DisableUsers(uuidList []string) error {
	var users []model.UserInfo
	if res := dao.GormDB.Model(model.UserInfo{}).Where("uuid in (?)", uuidList).Find(&users); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	for _, user := range users {
		user.Status = user_status_enum.DISABLE
		if res := dao.GormDB.Save(&user); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		// 禁用即撤销该用户全部 Refresh Token，并断开其在线连接。
		if err := auth.RevokeAll(user.Uuid); err != nil {
			zlog.Error(err.Error())
		}
		chat.KickOut(user.Uuid)
		var sessionList []model.Session
		if res := dao.GormDB.Where("send_id = ? or receive_id = ?", user.Uuid, user.Uuid).Find(&sessionList); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
		for _, session := range sessionList {
			var deletedAt gorm.DeletedAt
			deletedAt.Time = time.Now()
			deletedAt.Valid = true
			session.DeletedAt = deletedAt
			if res := dao.GormDB.Save(&session); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
	}
	// 删除所有"contact_user_list"开头的key
	//if err := myredis.DelKeysWithPrefix("contact_user_list"); err != nil {
	//	zlog.Error(err.Error())
	//}
	return nil
}

// DeleteUsers 删除用户
// 用户是否启用禁用需要实时更新contact_user_list状态，所以redis的contact_user_list需要删除
func (u *userInfoService) DeleteUsers(uuidList []string) error {
	var users []model.UserInfo
	if res := dao.GormDB.Model(model.UserInfo{}).Where("uuid in (?)", uuidList).Find(&users); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	for _, user := range users {
		user.DeletedAt.Valid = true
		user.DeletedAt.Time = time.Now()
		if res := dao.GormDB.Save(&user); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}

		// 删除会话
		var sessionList []model.Session
		if res := dao.GormDB.Where("send_id = ? or receive_id = ?", user.Uuid, user.Uuid).Find(&sessionList); res.Error != nil {
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				zlog.Info(res.Error.Error())
			} else {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
		for _, session := range sessionList {
			var deletedAt gorm.DeletedAt
			deletedAt.Time = time.Now()
			deletedAt.Valid = true
			session.DeletedAt = deletedAt
			if res := dao.GormDB.Save(&session); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}

		// 删除联系人
		var contactList []model.UserContact
		if res := dao.GormDB.Where("user_id = ? or contact_id = ?", user.Uuid, user.Uuid).Find(&contactList); res.Error != nil {
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				zlog.Info(res.Error.Error())
			} else {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
		for _, contact := range contactList {
			var deletedAt gorm.DeletedAt
			deletedAt.Time = time.Now()
			deletedAt.Valid = true
			contact.DeletedAt = deletedAt
			if res := dao.GormDB.Save(&contact); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}

		// 删除申请记录
		var applyList []model.ContactApply
		if res := dao.GormDB.Where("user_id = ? or contact_id = ?", user.Uuid, user.Uuid).Find(&applyList); res.Error != nil {
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				zlog.Info(res.Error.Error())
			} else {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}
		for _, apply := range applyList {
			var deletedAt gorm.DeletedAt
			deletedAt.Time = time.Now()
			deletedAt.Valid = true
			apply.DeletedAt = deletedAt
			if res := dao.GormDB.Save(&apply); res.Error != nil {
				zlog.Error(res.Error.Error())
				return apperr.SystemError(res.Error)
			}
		}

	}
	// 删除所有"contact_user_list"开头的key
	//if err := myredis.DelKeysWithPrefix("contact_user_list"); err != nil {
	//	zlog.Error(err.Error())
	//}
	return nil
}

// GetUserInfo 获取用户信息
func (u *userInfoService) GetUserInfo(uuid string) (*respond.GetUserInfoRespond, error) {
	// redis
	zlog.Info(uuid)
	rspString, err := myredis.GetKeyNilIsErr("user_info_" + uuid)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			zlog.Info(err.Error())
			var user model.UserInfo
			if res := dao.GormDB.Where("uuid = ?", uuid).Find(&user); res.Error != nil {
				zlog.Error(res.Error.Error())
				return nil, apperr.SystemError(res.Error)
			}
			rsp := respond.GetUserInfoRespond{
				Uuid:      user.Uuid,
				Telephone: user.Telephone,
				Nickname:  user.Nickname,
				Avatar:    user.Avatar,
				Birthday:  user.Birthday,
				Email:     user.Email,
				Gender:    user.Gender,
				Signature: user.Signature,
				CreatedAt: user.CreatedAt.Format("2006-01-02 15:04:05"),
				IsAdmin:   user.IsAdmin,
				Status:    user.Status,
			}
			return &rsp, nil
		} else {
			zlog.Error(err.Error())
			return nil, apperr.SystemError(err)
		}
	}
	var rsp respond.GetUserInfoRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return &rsp, nil
}

// SetAdmin 设置管理员
func (u *userInfoService) SetAdmin(uuidList []string, isAdmin int8) error {
	var users []model.UserInfo
	if res := dao.GormDB.Where("uuid = (?)", uuidList).Find(&users); res.Error != nil {
		zlog.Error(res.Error.Error())
		return apperr.SystemError(res.Error)
	}
	for _, user := range users {
		user.IsAdmin = isAdmin
		if res := dao.GormDB.Save(&user); res.Error != nil {
			zlog.Error(res.Error.Error())
			return apperr.SystemError(res.Error)
		}
	}
	return nil
}
