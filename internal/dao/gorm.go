package dao

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"gochat/internal/config"
	"gochat/internal/model"
	"gochat/pkg/zlog"
)

var (
	// GormDB 保存全局数据库连接，供 service / controller 复用。
	GormDB *gorm.DB
	// dbMu 保护 Init 只会在并发场景下真正执行一次初始化。
	dbMu sync.Mutex
)

func Init() error {
	dbMu.Lock()
	defer dbMu.Unlock()

	// 已经初始化过就直接返回，避免重复建连和重复迁移。
	if GormDB != nil {
		return nil
	}

	// 从全局配置中读取 MySQL 参数，组装 DSN 并打开连接。
	db, err := open(config.GetConfig())
	if err != nil {
		return err
	}

	// 启动时自动同步表结构，保证基础表存在。
	if err := autoMigrate(db); err != nil {
		return err
	}

	// 初始化成功后把连接保存到全局变量。
	GormDB = db
	return nil
}

func MustInit() {
	// 确保初始化成功,报错了直接把服务停掉
	if err := Init(); err != nil {
		zlog.Fatal(err.Error())
	}
}

func BuildDSN(mysqlConfig config.MysqlConfig) string {
	// 默认按 tcp 方式拼接 DSN。
	base := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		mysqlConfig.User,
		mysqlConfig.Password,
		mysqlConfig.Host,
		mysqlConfig.Port,
		mysqlConfig.DatabaseName,
	)

	// 如果显式配置 network=unix，就切换成 Unix Socket 连接方式。
	if strings.EqualFold(mysqlConfig.Network, "unix") {
		base = fmt.Sprintf(
			"%s:%s@unix(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			mysqlConfig.User,
			mysqlConfig.Password,
			mysqlConfig.SocketPath,
			mysqlConfig.DatabaseName,
		)
	}

	return base
}

func open(cfg *config.Config) (*gorm.DB, error) {
	// 根据配置组装 DSN，再交给 gorm.Open 创建连接。
	dsn := BuildDSN(cfg.MysqlConfig)
	// 生产 / 压测下关闭 GORM 逐条 SQL 日志（控制台 I/O 会成为高吞吐路径的瓶颈），
	// 业务错误仍由上层 zlog 记录。
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	// 连接池配置：高频接口全靠缓存兜底，DB 连接应稳定复用，
	// 避免请求间隙连接被回收导致频繁重建（每次新建连接都会触发 SELECT DATABASE()）。
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(50)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

// dedupTelephone 在添加手机号唯一索引前清理存量重复手机号（保留最小 id 的行）。
// 这是数据修复而非业务逻辑：仅当同手机号存在多行时生效，正常数据零影响。
func dedupTelephone(db *gorm.DB) {
	result := db.Exec(
		"DELETE u1 FROM user_info u1 INNER JOIN user_info u2 " +
			"ON u1.telephone = u2.telephone AND u1.id > u2.id",
	)
	if result.Error != nil {
		zlog.Error("清理重复手机号失败: " + result.Error.Error())
		return
	}
	if result.RowsAffected > 0 {
		zlog.Info("清理重复手机号", zap.Int64("rows", result.RowsAffected))
	}

	// 旧版本用普通索引（同名 idx_user_info_telephone），会与新的唯一索引冲突，
	// 迁移前先删除旧索引，避免 AutoMigrate 报 Duplicate key name。
	var legacyCount int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM information_schema.statistics " +
			"WHERE table_schema = DATABASE() AND table_name = 'user_info' " +
			"AND index_name = 'idx_user_info_telephone' AND non_unique = 1",
	).Scan(&legacyCount).Error; err != nil {
		zlog.Error("检查旧手机号索引失败: " + err.Error())
		return
	}
	if legacyCount > 0 {
		if err := db.Exec("ALTER TABLE user_info DROP INDEX idx_user_info_telephone").Error; err != nil {
			zlog.Error("删除旧手机号索引失败: " + err.Error())
			return
		}
		zlog.Info("已删除旧版普通索引 idx_user_info_telephone")
	}
}

func autoMigrate(db *gorm.DB) error {
	// user_info.telephone 将升级为唯一索引，先做存量去重，避免迁移失败。
	dedupTelephone(db)
	// 把项目需要的 model 一次性迁移到数据库。
	// 这里属于“启动即保证表结构可用”的策略。
	return db.AutoMigrate(
		&model.UserInfo{},
		&model.GroupInfo{},
		&model.UserContact{},
		&model.Session{},
		&model.ContactApply{},
		&model.Message{},
	)
}
