package pgsql

import (
	"database/sql"
	"fmt"
	"github.com/polarismesh/polaris/common/log"
	"github.com/polarismesh/polaris/plugin"
	"time"
)

// BaseDB 对sql.DB的封装
type BaseDB struct {
	*sql.DB
	cfg            *dbConfig
	isolationLevel sql.IsolationLevel
	parsePwd       plugin.ParsePassword
}

// dbConfig store的配置
type dbConfig struct {
	dbType           string
	dbUser           string
	dbPwd            string
	dbAddr           string
	dbPort           int
	dbName           string
	maxOpenConns     int
	maxIdleConns     int
	connMaxLifetime  int
	txIsolationLevel int
}

// NewBaseDB 新建一个BaseDB
func NewBaseDB(cfg *dbConfig, parsePwd plugin.ParsePassword) (*BaseDB, error) {
	baseDb := &BaseDB{cfg: cfg, parsePwd: parsePwd}
	if cfg.txIsolationLevel > 0 {
		baseDb.isolationLevel = sql.IsolationLevel(cfg.txIsolationLevel)
		log.Infof("[Store][database] use isolation level: %s", baseDb.isolationLevel.String())
	}

	//if err := baseDb

	return nil, nil
}

// desc
// @param
// @author zhangming
// @date 2023/5/28-01:46
func (b *BaseDB) openDatabase() error {
	c := b.cfg

	// 密码解析插件
	if b.parsePwd != nil {
		pwd, err := b.parsePwd.ParsePassword(c.dbPwd)
		if err != nil {
			log.Errorf("[Store][database][ParsePwdPlugin] parse password err: %s", err.Error())
			return err
		}
		c.dbPwd = pwd
	}

	dns := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", c.dbAddr, c.dbPort, "postgres", "aaaaaa", "postgres")

	db, err := sql.Open(c.dbType, dns)
	if err != nil {
		log.Errorf("[Store][database] sql open err: %s", err.Error())
		return err
	}
	if pingErr := db.Ping(); pingErr != nil {
		log.Errorf("[Store][database] database ping err: %s", pingErr.Error())
		return pingErr
	}
	if c.maxOpenConns > 0 {
		log.Infof("[Store][database] db set max open conns: %d", c.maxOpenConns)
		db.SetMaxOpenConns(c.maxOpenConns)
	}
	if c.maxIdleConns > 0 {
		log.Infof("[Store][database] db set max idle conns: %d", c.maxIdleConns)
		db.SetMaxIdleConns(c.maxIdleConns)
	}
	if c.connMaxLifetime > 0 {
		log.Infof("[Store][database] db set conn max life time: %d", c.connMaxLifetime)
		db.SetConnMaxLifetime(time.Second * time.Duration(c.connMaxLifetime))
	}

	b.DB = db

	return nil
}
