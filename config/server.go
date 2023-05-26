/**
 * Tencent is pleased to support the open source community by making Polaris available.
 *
 * Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.
 *
 * Licensed under the BSD 3-Clause License (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://opensource.org/licenses/BSD-3-Clause
 *
 * Unless required by applicable law or agreed to in writing, software distributed
 * under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
 * CONDITIONS OF ANY KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations under the License.
 */

package config

import (
	"context"
	"errors"
	"time"

	apiconfig "github.com/polarismesh/specification/source/go/api/v1/config_manage"
	"go.uber.org/zap"

	"github.com/polarismesh/polaris/auth"
	"github.com/polarismesh/polaris/cache"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/common/utils"
	"github.com/polarismesh/polaris/namespace"
	"github.com/polarismesh/polaris/plugin"
	"github.com/polarismesh/polaris/store"
)

var _ ConfigCenterServer = (*Server)(nil)

const (
	eventTypePublishConfigFile  = "PublishConfigFile"
	defaultExpireTimeAfterWrite = 60 * 60 // expire after 1 hour
)

var (
	server       ConfigCenterServer
	originServer = &Server{}
)

// Config 配置中心模块启动参数
type Config struct {
	Open  bool                   `yaml:"open"`
	Cache map[string]interface{} `yaml:"cache"`
}

// Server 配置中心核心服务
type Server struct {
	storage           store.Store
	fileCache         cache.FileCache
	caches            *cache.CacheManager
	watchCenter       *watchCenter
	connManager       *connManager
	namespaceOperator namespace.NamespaceOperateServer
	initialized       bool

	history       plugin.History
	cryptoManager *plugin.CryptoManager
	hooks         []ResourceHook
}

// Initialize 初始化配置中心模块
func Initialize(ctx context.Context, config Config, s store.Store, cacheMgn *cache.CacheManager,
	namespaceOperator namespace.NamespaceOperateServer, userMgn auth.UserServer, strategyMgn auth.StrategyServer) error {
	if !config.Open {
		originServer.initialized = true
		return nil
	}

	if originServer.initialized {
		return nil
	}

	err := originServer.initialize(ctx, config, s, namespaceOperator, cacheMgn)
	if err != nil {
		return err
	}

	server = newServerAuthAbility(originServer, userMgn, strategyMgn)

	originServer.initialized = true
	return nil
}

func (s *Server) initialize(ctx context.Context, config Config, ss store.Store,
	namespaceOperator namespace.NamespaceOperateServer, cacheMgn *cache.CacheManager) error {

	s.storage = ss
	s.namespaceOperator = namespaceOperator
	s.fileCache = cacheMgn.ConfigFile()

	// 初始化事件中心
	eventCenter := NewEventCenter()
	s.watchCenter = NewWatchCenter(eventCenter)

	// 初始化连接管理器
	connMng := NewConfigConnManager(ctx, s.watchCenter)
	s.connManager = connMng

	// 获取History插件，注意：插件的配置在bootstrap已经设置好
	s.history = plugin.GetHistory()
	if s.history == nil {
		log.Warnf("Not Found History Log Plugin")
	}
	// 获取Crypto插件
	s.cryptoManager = plugin.GetCryptoManager()
	if s.cryptoManager == nil {
		log.Warnf("Not Found Crypto Plugin")
	}

	// 初始化发布事件扫描器
	if err := initReleaseMessageScanner(ctx, ss, s.fileCache, eventCenter, time.Second); err != nil {
		log.Error("[Config][Server] init release message scanner error. ", zap.Error(err))
		return errors.New("init config module error")
	}

	s.caches = cacheMgn

	log.Infof("[Config][Server] startup config module success.")
	return nil
}

// GetServer 获取已经初始化好的ConfigServer
func GetServer() (ConfigCenterServer, error) {
	if !originServer.initialized {
		return nil, errors.New("config server has not done initialize")
	}

	return server, nil
}

func GetOriginServer() (*Server, error) {
	if !originServer.initialized {
		return nil, errors.New("config server has not done initialize")
	}

	return originServer, nil
}

// WatchCenter 获取监听事件中心
func (s *Server) WatchCenter() *watchCenter {
	return s.watchCenter
}

// Cache 获取配置中心缓存模块
func (s *Server) Cache() cache.FileCache {
	return s.fileCache
}

// ConnManager 获取配置中心连接管理器
func (s *Server) ConnManager() *connManager {
	return s.connManager
}

// SetResourceHooks 设置资源钩子
func (s *Server) SetResourceHooks(hooks ...ResourceHook) {
	s.hooks = hooks
}

func (s *Server) afterConfigGroupResource(ctx context.Context, req *apiconfig.ConfigFileGroup) error {
	event := &ResourceEvent{
		ConfigGroup: req,
	}

	for _, hook := range s.hooks {
		if err := hook.After(ctx, model.RConfigGroup, event); err != nil {
			return err
		}
	}
	return nil
}

// RecordHistory server对外提供history插件的简单封装
func (s *Server) RecordHistory(ctx context.Context, entry *model.RecordEntry) {
	// 如果插件没有初始化，那么不记录history
	if s.history == nil {
		return
	}
	// 如果数据为空，则不需要打印了
	if entry == nil {
		return
	}

	fromClient, _ := ctx.Value(utils.ContextIsFromClient).(bool)
	if fromClient {
		return
	}
	// 调用插件记录history
	s.history.Record(entry)
}
