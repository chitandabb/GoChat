package sms

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gochat/internal/config"
	"gochat/internal/service/redis"
	"gochat/pkg/apperr"
	"gochat/pkg/util/random"
	"gochat/pkg/zlog"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dypnsapi20170525 "github.com/alibabacloud-go/dypnsapi-20170525/v3/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"go.uber.org/zap"
)

var smsClient *dypnsapi20170525.Client

// devCodeKeyPrefix 开发模式验证码的 Redis key 前缀（未配置阿里云短信时兜底，见 sendDevVerificationCode）。
const devCodeKeyPrefix = "sms_dev_code:"

// smsConfigured 判断是否配置了可用的阿里云短信（AK/SK + 签名 + 模板都齐全才走真实发送）。
func smsConfigured(authCfg config.AuthCodeConfig) bool {
	return strings.TrimSpace(authCfg.AccessKeyID) != "" &&
		strings.TrimSpace(authCfg.AccessKeySecret) != "" &&
		strings.TrimSpace(authCfg.SignName) != "" &&
		strings.TrimSpace(authCfg.TemplateCode) != ""
}

// useDevMode 是否使用开发模式验证码：
// 1. 显式配置 DevMode=true 时（演示/联调）强制开发模式，即使 AK 齐全；
// 2. 否则仅当阿里云配置缺失时自动降级，保证未接入短信也能登录/注册。
func useDevMode(authCfg config.AuthCodeConfig) bool {
	return authCfg.DevMode || !smsConfigured(authCfg)
}

// devCodeKey 返回开发模式验证码在 Redis 中的 key。
func devCodeKey(telephone string) string {
	return devCodeKeyPrefix + telephone
}

// sendDevVerificationCode 开发模式发送验证码：
// 仅当阿里云短信配置缺失时启用——生成 6 位验证码写入 Redis（TTL 同 validTime），
// 并在服务端日志打印，便于本地演示注册/短信登录；不调用真实短信通道。
// 一旦配置齐全，本函数不会被调用，行为与原来完全一致。
func sendDevVerificationCode(telephone string, authCfg config.AuthCodeConfig) error {
	code := fmt.Sprintf("%06d", random.GetRandomInt(6))
	if err := redis.SetKeyEx(devCodeKey(telephone), code, time.Duration(authCfg.ValidTime)*time.Second); err != nil {
		zlog.Error(err.Error())
		return apperr.SystemError(err)
	}
	// 开发模式仅用于本地演示：验证码打到服务端日志，由演示者查日志获取。
	zlog.Warn("【开发模式】短信验证码（未配置阿里云短信时生效，生产环境请配置 AccessKey）",
		zap.String("telephone", maskTelephone(telephone)),
		zap.String("code", code),
	)
	return nil
}

// checkDevVerificationCode 开发模式校验验证码：Redis 有该手机的开发验证码则本地比对，
// 命中后删除（一次性使用）。返回 (是否走开发校验, error)。
func checkDevVerificationCode(telephone, verifyCode string) (bool, error) {
	value, err := redis.GetKey(devCodeKey(telephone))
	if err != nil {
		zlog.Error(err.Error())
		return false, apperr.SystemError(err)
	}
	if value == "" {
		// 没有开发验证码，走真实阿里云校验
		return false, nil
	}
	if value != verifyCode {
		return true, apperr.Biz("验证码错误")
	}
	if err := redis.DelKeys(devCodeKey(telephone)); err != nil {
		zlog.Error(err.Error())
		return true, apperr.SystemError(err)
	}
	return true, nil
}

// createClient 使用 AK&SK 初始化号码认证客户端。
func createClient() (result *dypnsapi20170525.Client, err error) {
	authCfg := config.GetConfig().AuthCodeConfig
	if smsClient == nil {
		zlog.Info(
			"初始化阿里云短信认证客户端",
			zap.String("endpoint", "dypnsapi.aliyuncs.com"),
		)
		openapiConfig := &openapi.Config{
			AccessKeyId:     tea.String(authCfg.AccessKeyID),
			AccessKeySecret: tea.String(authCfg.AccessKeySecret),
		}
		openapiConfig.Endpoint = tea.String("dypnsapi.aliyuncs.com")
		smsClient, err = dypnsapi20170525.NewClient(openapiConfig)
		if err != nil {
			zlog.Error("初始化阿里云短信认证客户端失败", zap.Error(err))
			return nil, err
		}
	}
	return smsClient, err
}

// SendVerificationCode 发送短信验证码。
// 开发模式（DevMode 显式开启，或阿里云配置缺失时自动降级）：验证码写 Redis + 服务端日志打印；
// 配置齐全且 DevMode=false 时走真实阿里云发送，行为不变。
func SendVerificationCode(telephone string) error {
	authCfg := config.GetConfig().AuthCodeConfig
	if useDevMode(authCfg) {
		return sendDevVerificationCode(telephone, authCfg)
	}

	client, err := createClient()
	if err != nil {
		return apperr.SystemError(err)
	}

	if err := validateAuthCodeConfig(authCfg); err != nil {
		return err
	}

	trimmedSchemeName := strings.TrimSpace(authCfg.SchemeName)
	zlog.Info(
		"开始发送短信验证码",
		zap.String("telephone", maskTelephone(telephone)),
		zap.String("scheme_name", trimmedSchemeName),
		zap.String("country_code", authCfg.CountryCode),
		zap.Int("valid_time_seconds", authCfg.ValidTime),
		zap.Int("interval_seconds", authCfg.Interval),
		zap.Int("code_length", authCfg.CodeLength),
	)

	templateParam, err := buildTemplateParam()
	if err != nil {
		zlog.Error(
			"构造短信模板参数失败",
			zap.String("telephone", maskTelephone(telephone)),
			zap.Error(err),
		)
		return apperr.SystemError(err)
	}

	request := &dypnsapi20170525.SendSmsVerifyCodeRequest{
		PhoneNumber:      tea.String(telephone),
		SignName:         tea.String(authCfg.SignName),
		TemplateCode:     tea.String(authCfg.TemplateCode),
		TemplateParam:    tea.String(templateParam),
		CountryCode:      tea.String(authCfg.CountryCode),
		CodeLength:       tea.Int64(int64(authCfg.CodeLength)),
		ValidTime:        tea.Int64(int64(authCfg.ValidTime)),
		DuplicatePolicy:  tea.Int64(int64(authCfg.DuplicatePolicy)),
		Interval:         tea.Int64(int64(authCfg.Interval)),
		CodeType:         tea.Int64(int64(authCfg.CodeType)),
		ReturnVerifyCode: tea.Bool(authCfg.ReturnVerifyCode),
		AutoRetry:        tea.Int64(int64(authCfg.AutoRetry)),
	}
	if trimmedSchemeName != "" {
		request.SchemeName = tea.String(trimmedSchemeName)
	}

	runtime := &dara.RuntimeOptions{}
	response, err := client.SendSmsVerifyCodeWithOptions(request, runtime)
	if err != nil {
		zlog.Error(
			"调用阿里云发送验证码接口失败",
			zap.String("telephone", maskTelephone(telephone)),
			zap.String("scheme_name", trimmedSchemeName),
			zap.Error(err),
		)
		return apperr.SystemError(err)
	}
	if err := validateSendResponse(response); err != nil {
		return err
	}

	zlog.Info(
		"短信验证码发送成功",
		zap.String("telephone", maskTelephone(telephone)),
		zap.String("scheme_name", trimmedSchemeName),
		zap.String("code", teaString(response.Body.Code)),
		zap.String("message", teaString(response.Body.Message)),
		zap.String("request_id", teaString(response.Body.RequestId)),
		zap.String("biz_id", sendBizID(response)),
	)

	return nil
}

// CheckVerificationCode 校验短信验证码。
// 开发模式（Redis 中存在开发验证码）下本地比对；否则由阿里云校验。
func CheckVerificationCode(telephone string, verifyCode string) error {
	if dev, err := checkDevVerificationCode(telephone, verifyCode); dev || err != nil {
		return err
	}

	authCfg := config.GetConfig().AuthCodeConfig
	// 开发模式校验后（Redis 无开发验证码）且配置缺失：明确报错而不是试图调用阿里云
	if useDevMode(authCfg) {
		return apperr.Biz("验证码无效或已过期，请重新获取")
	}

	client, err := createClient()
	if err != nil {
		return apperr.SystemError(err)
	}

	if err := validateAuthCodeConfig(authCfg); err != nil {
		return err
	}

	trimmedSchemeName := strings.TrimSpace(authCfg.SchemeName)
	zlog.Info(
		"开始校验短信验证码",
		zap.String("telephone", maskTelephone(telephone)),
		zap.String("scheme_name", trimmedSchemeName),
		zap.String("country_code", authCfg.CountryCode),
	)

	request := &dypnsapi20170525.CheckSmsVerifyCodeRequest{
		PhoneNumber:    tea.String(telephone),
		VerifyCode:     tea.String(verifyCode),
		CountryCode:    tea.String(authCfg.CountryCode),
		CaseAuthPolicy: tea.Int64(int64(authCfg.CaseAuthPolicy)),
	}
	if trimmedSchemeName != "" {
		request.SchemeName = tea.String(trimmedSchemeName)
	}

	runtime := &dara.RuntimeOptions{}
	response, err := client.CheckSmsVerifyCodeWithOptions(request, runtime)
	if err != nil {
		zlog.Error(
			"调用阿里云校验验证码接口失败",
			zap.String("telephone", maskTelephone(telephone)),
			zap.String("scheme_name", trimmedSchemeName),
			zap.Error(err),
		)
		return apperr.SystemError(err)
	}
	if err := validateCheckResponse(response); err != nil {
		return err
	}

	zlog.Info(
		"短信验证码校验成功",
		zap.String("telephone", maskTelephone(telephone)),
		zap.String("scheme_name", trimmedSchemeName),
		zap.String("code", teaString(response.Body.Code)),
		zap.String("message", teaString(response.Body.Message)),
		zap.String("verify_result", checkVerifyResult(response)),
	)

	return nil
}

func validateAuthCodeConfig(authCfg config.AuthCodeConfig) error {
	if strings.TrimSpace(authCfg.AccessKeyID) == "" || strings.TrimSpace(authCfg.AccessKeySecret) == "" {
		zlog.Warn("短信认证 AccessKey 配置不完整")
		return apperr.Biz("短信认证配置不完整，请检查 AccessKey")
	}
	if strings.TrimSpace(authCfg.SignName) == "" || strings.TrimSpace(authCfg.TemplateCode) == "" {
		zlog.Warn("短信认证签名或模板配置不完整")
		return apperr.Biz("短信认证配置不完整，请检查签名和模板")
	}
	return nil
}

func buildTemplateParam() (string, error) {
	authConfig := config.GetConfig().AuthCodeConfig
	validMin := authConfig.ValidTime / 60
	if authConfig.ValidTime%60 != 0 {
		validMin++
	}
	templateParam := map[string]string{
		"code": "##code##",
		"min":  strconv.Itoa(validMin),
	}
	templateParamBytes, err := json.Marshal(templateParam)
	if err != nil {
		return "", err
	}
	return string(templateParamBytes), nil
}

func validateSendResponse(response *dypnsapi20170525.SendSmsVerifyCodeResponse) error {
	if response == nil || response.Body == nil {
		zlog.Error("阿里云发送验证码返回体为空")
		return apperr.SystemError(nil)
	}
	if response.Body.Success == nil || !tea.BoolValue(response.Body.Success) {
		zlog.Warn(
			"阿里云发送验证码失败",
			zap.String("code", teaString(response.Body.Code)),
			zap.String("message", teaString(response.Body.Message)),
			zap.String("request_id", teaString(response.Body.RequestId)),
		)
		return apperr.Biz(bizMessage(teaString(response.Body.Message), "验证码发送失败"))
	}
	if response.Body.Code == nil || tea.StringValue(response.Body.Code) != "OK" {
		zlog.Warn(
			"阿里云发送验证码返回非 OK 状态",
			zap.String("code", teaString(response.Body.Code)),
			zap.String("message", teaString(response.Body.Message)),
			zap.String("request_id", teaString(response.Body.RequestId)),
		)
		return apperr.Biz(bizMessage(teaString(response.Body.Message), "验证码发送失败"))
	}
	return nil
}

func validateCheckResponse(response *dypnsapi20170525.CheckSmsVerifyCodeResponse) error {
	if response == nil || response.Body == nil {
		zlog.Error("阿里云校验验证码返回体为空")
		return apperr.SystemError(nil)
	}
	if response.Body.Success == nil || !tea.BoolValue(response.Body.Success) {
		zlog.Warn(
			"阿里云校验验证码失败",
			zap.String("code", teaString(response.Body.Code)),
			zap.String("message", teaString(response.Body.Message)),
			zap.String("verify_result", checkVerifyResult(response)),
		)
		return apperr.Biz(bizMessage(teaString(response.Body.Message), "验证码校验失败"))
	}
	if response.Body.Code == nil || tea.StringValue(response.Body.Code) != "OK" {
		zlog.Warn(
			"阿里云校验验证码返回非 OK 状态",
			zap.String("code", teaString(response.Body.Code)),
			zap.String("message", teaString(response.Body.Message)),
			zap.String("verify_result", checkVerifyResult(response)),
		)
		return apperr.Biz(bizMessage(teaString(response.Body.Message), "验证码校验失败"))
	}
	if response.Body.Model == nil || response.Body.Model.VerifyResult == nil || tea.StringValue(response.Body.Model.VerifyResult) != "PASS" {
		zlog.Warn(
			"短信验证码校验未通过",
			zap.String("code", teaString(response.Body.Code)),
			zap.String("message", teaString(response.Body.Message)),
			zap.String("verify_result", checkVerifyResult(response)),
		)
		return apperr.Biz("验证码不正确，请重试")
	}
	return nil
}

func maskTelephone(telephone string) string {
	trimmedTelephone := strings.TrimSpace(telephone)
	if len(trimmedTelephone) <= 7 {
		return trimmedTelephone
	}
	return trimmedTelephone[:3] + "****" + trimmedTelephone[len(trimmedTelephone)-4:]
}

// bizMessage 阿里云返回 message 为空时使用兜底文案。
func bizMessage(message, fallback string) string {
	if strings.TrimSpace(message) == "" {
		return fallback
	}
	return message
}

func teaString(value *string) string {
	if value == nil {
		return ""
	}
	return tea.StringValue(value)
}

func sendBizID(response *dypnsapi20170525.SendSmsVerifyCodeResponse) string {
	if response == nil || response.Body == nil || response.Body.Model == nil || response.Body.Model.BizId == nil {
		return ""
	}
	return tea.StringValue(response.Body.Model.BizId)
}

func checkVerifyResult(response *dypnsapi20170525.CheckSmsVerifyCodeResponse) string {
	if response == nil || response.Body == nil || response.Body.Model == nil || response.Body.Model.VerifyResult == nil {
		return ""
	}
	return tea.StringValue(response.Body.Model.VerifyResult)
}
