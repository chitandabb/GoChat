# 个人开发者 Golang接入 短信验证码 服务

## 1. 背景

本项目需要支持手机号验证码登录和注册。

这次接入选择的是阿里云号码认证里的短信验证码能力，也就是：

- 发送验证码：`SendSmsVerifyCode`
- 校验验证码：`CheckSmsVerifyCode`

这条路线的特点是：

- 验证码由阿里云生成
- 验证码由阿里云校验
- 后端只负责发起发送请求和校验请求
- 业务层不需要自己维护验证码内容

本次改造的目标是：

1. 保留现有接口，不改前端调用方式
2. 把短信服务能力集中在 `internal/service/sms`
3. 让配置、调用、日志都比较清晰，方便本地调试

## 2. 前置工作

在正式写代码前，先要完成下面几件事。

### 2.1 确认接入能力

先确认不是接普通短信发送 SDK，而是接“阿里云短信验证码服务”这条链路。  
对应到代码里，就是要使用：

- `SendSmsVerifyCode`
- `CheckSmsVerifyCode`

所以 Go SDK 依赖改成：

```go
github.com/alibabacloud-go/dypnsapi-20170525/v3 v3.0.0
```

然后执行：

```bash
go mod tidy
```

### 2.2 补配置结构

为了避免把参数写死在代码里，需要先在配置里增加短信服务相关字段：

```go
type AuthCodeConfig struct {
    AccessKeyID      string `toml:"accessKeyID"`
    AccessKeySecret  string `toml:"accessKeySecret"`
    SignName         string `toml:"signName"`
    TemplateCode     string `toml:"templateCode"`
    SchemeName       string `toml:"schemeName"`
    CountryCode      string `toml:"countryCode"`
    CodeLength       int    `toml:"codeLength"`
    ValidTime        int    `toml:"validTime"`
    DuplicatePolicy  int    `toml:"duplicatePolicy"`
    Interval         int    `toml:"interval"`
    CodeType         int    `toml:"codeType"`
    CaseAuthPolicy   int    `toml:"caseAuthPolicy"`
    ReturnVerifyCode bool   `toml:"returnVerifyCode"`
    AutoRetry        int    `toml:"autoRetry"`
}
```

其中重点配置是：

- `accessKeyID`
- `accessKeySecret`
- `signName`
- `templateCode`

其余字段大多是控制验证码长度、有效期、频率限制和校验策略。

### 2.3 设置默认值

为了防止配置文件漏填一些非核心字段，代码里还补了一组默认值：

- `CountryCode = "86"`
- `CodeLength = 6`
- `ValidTime = 300`
- `Interval = 60`
- `DuplicatePolicy = 1`
- `CodeType = 1`
- `CaseAuthPolicy = 1`
- `AutoRetry = 1`

这样即使配置里没写全，服务也能先按一套合理默认值跑起来。

### 2.4 准备短信模板

阿里云控制台里要先准备好短信模板。  
代码里传给模板的变量名，必须和模板里的占位符一致。

比如模板里如果用了验证码和“几分钟内有效”两个变量，那么代码里就要传：

```json
{
  "code": "##code##",
  "min": "5"
}
```

这里的 `code`、`min` 必须和模板变量名一致，不能自己随便改。

## 3. 代码实现以及讲解

### 3.1 配置入口

短信服务配置统一从全局配置读取，也就是：

- `config.GetConfig().AuthCodeConfig`

这样短信服务本身不关心配置文件是怎么加载的，只关心“当前拿到的配置是什么”。

这一步的意义是把“配置加载”和“短信调用”分开，减少耦合。

### 3.2 创建阿里云客户端

在 `internal/service/sms/auth_code_service.go` 里，先封装了一个 `createClient()`：

```go
func createClient() (result *dypnsapi20170525.Client, err error)
```

这个函数做的事情是：

1. 从配置里读取 `AccessKeyID` 和 `AccessKeySecret`
2. 设置 endpoint：`dypnsapi.aliyuncs.com`
3. 初始化阿里云客户端
4. 用全局变量缓存客户端，避免重复创建

这里还会用到阿里云 SDK 里的 `tea` 包，例如：

```go
tea.String("abc")
tea.Int64(60)
tea.Bool(true)
```

它的作用就是把普通值转成指针值，因为阿里云 SDK 的很多请求字段都是 `*string`、`*int64`、`*bool` 这种类型。

读取响应时也会用：

```go
tea.StringValue(ptr)
tea.BoolValue(ptr)
```

用来把指针安全地取回普通值。

### 3.3 发送验证码实现

发送验证码入口是：

```go
func SendVerificationCode(telephone string) (string, int)
```

整体步骤如下：

1. 创建阿里云客户端
2. 校验短信配置是否完整
3. 构造模板参数
4. 组装 `SendSmsVerifyCodeRequest`
5. 调用阿里云发送接口
6. 校验响应结果
7. 返回业务消息和状态码

其中模板参数构造逻辑是重点：

```go
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
```

这里有两个关键点：

1. `code` 传的是 `"##code##"`，表示验证码由阿里云生成
2. `ValidTime` 配置单位是秒，但模板里通常写“几分钟内有效”，所以代码里要先换算成分钟

### 3.4 校验验证码实现

校验验证码入口是：

```go
func CheckVerificationCode(telephone string, verifyCode string) (string, int)
```

整体步骤如下：

1. 创建阿里云客户端
2. 校验配置
3. 组装 `CheckSmsVerifyCodeRequest`
4. 调用阿里云校验接口
5. 检查响应是否成功
6. 检查 `VerifyResult` 是否为 `PASS`

只有当：

- `response.Body.Success == true`
- `response.Body.Code == "OK"`
- `response.Body.Model.VerifyResult == "PASS"`

这三个条件都满足时，才算验证码校验成功。

### 3.5 业务层接入

业务层没有直接碰阿里云 SDK，而是继续只调用短信服务封装的方法。

短信登录里调用：

```go
sms.CheckVerificationCode(req.Telephone, req.SmsCode)
```

注册里调用：

```go
sms.CheckVerificationCode(registerReq.Telephone, registerReq.SmsCode)
```

发送验证码里调用：

```go
sms.SendVerificationCode(telephone)
```

这样业务层只知道“发验证码”和“校验验证码”这两个动作，不关心底层到底是哪家服务商，也不关心请求结构体长什么样。

这就是这次实现里比较重要的一个点：  
**把供应商 SDK 收口在 `internal/service/sms`，不要扩散到业务层。**

### 3.6 响应校验

因为云服务调用最容易出问题的地方，就是“请求发出去了，但返回不是成功”，所以代码里单独封装了响应校验逻辑。

发送时会校验：

- 返回体是否为空
- `Success` 是否为 `true`
- `Code` 是否为 `"OK"`

校验验证码时除了上面这些，还会额外校验：

- `VerifyResult` 是否为 `"PASS"`

这样做的好处是，错误能尽量在短信服务层就被拦住，而不是把不完整响应继续传到业务层。

### 3.7 日志补充

为了方便联调，`auth_code_service.go` 里补了比较完整的日志，主要包括：

- 初始化客户端
- 开始发送验证码
- 构造模板参数失败
- 调用发送接口失败
- 发送接口返回非成功状态
- 发送成功
- 开始校验验证码
- 调用校验接口失败
- 校验接口返回非成功状态
- 验证码校验不通过
- 验证码校验成功

日志里会记录：

- 脱敏手机号
- `schemeName`
- `countryCode`
- 阿里云返回的 `code`
- 阿里云返回的 `message`
- `request_id`
- `biz_id`
- `verify_result`

其中手机号做了脱敏处理，不会直接明文写入日志；用户输入的验证码也不会被打印到日志中。

### 3.8 联调时遇到的问题

这次联调里，比较典型的问题有两个。

第一个是：

```text
check frequency failed
```

这通常说明阿里云的频率限制生效了，也就是说请求大概率已经打到阿里云了。  
这时优先检查 `interval` 配置和同一个手机号的重复请求频率。

第二个是：

```text
请检查模板内容与模板参数是否匹配
```

这个问题最后确认是模板参数没传全。  
如果模板里有 `min` 变量，而代码里只传了 `code`，就会报这个错。  
所以最终需要在 `TemplateParam` 里把 `min` 一起补上。

### 3.9 验证方式

本次改造完成后，至少要确认下面几件事：

1. 项目能正常编译
2. 发送验证码接口能打通
3. 校验验证码接口能返回正确结果
4. 模板参数和模板变量能对应上
5. 日志里能看清调用链路和失败原因

定向测试命令可以用：

```bash
go test ./internal/service/sms ./internal/service/gorm ./internal/config ./test/config
```

以上这套实现的核心思路可以概括成一句话：

**后端负责调用，阿里云负责生成和校验验证码，业务层只依赖统一的短信服务接口。**
