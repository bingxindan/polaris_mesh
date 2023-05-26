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

package service

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/polarismesh/polaris/auth"
	"github.com/polarismesh/polaris/common/eventhub"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/plugin"
)

const (
	// MaxBatchSize max batch size
	MaxBatchSize = 100
	// MaxQuerySize max query size
	MaxQuerySize = 100
)

const (
	// SystemNamespace polaris system namespace
	SystemNamespace = "Polaris"
	// DefaultNamespace default namespace
	DefaultNamespace = "default"
	// ProductionNamespace default namespace
	ProductionNamespace = "Production"
	// DefaultTLL default ttl
	DefaultTLL = 5
)

var (
	server       DiscoverServer
	namingServer *Server = new(Server)
	once                 = sync.Once{}
	finishInit           = false
)

// Config 核心逻辑层配置
type Config struct {
	Auth  map[string]interface{} `yaml:"auth"`
	Batch map[string]interface{} `yaml:"batch"`
}

// Initialize 初始化
func Initialize(ctx context.Context, namingOpt *Config, opts ...InitOption) error {
	var err error
	once.Do(func() {
		err = initialize(ctx, namingOpt, opts...)
	})

	if err != nil {
		return err
	}

	finishInit = true
	return nil
}

// GetServer 获取已经初始化好的Server
func GetServer() (DiscoverServer, error) {
	if !finishInit {
		return nil, errors.New("server has not done InitializeServer")
	}

	return server, nil
}

// GetOriginServer 获取已经初始化好的Server
func GetOriginServer() (*Server, error) {
	if !finishInit {
		return nil, errors.New("server has not done InitializeServer")
	}

	return namingServer, nil
}

// 内部初始化函数
func initialize(ctx context.Context, namingOpt *Config, opts ...InitOption) error {
	// l5service
	namingServer.l5service = &l5service{}
	namingServer.createServiceSingle = &singleflight.Group{}

	for i := range opts {
		opts[i](namingServer)
	}

	// 插件初始化
	pluginInitialize()

	userMgn, err := auth.GetUserServer()
	if err != nil {
		return err
	}
	strategyMgn, err := auth.GetStrategyServer()
	if err != nil {
		return err
	}

	server = newServerAuthAbility(namingServer, userMgn, strategyMgn)

	return nil
}

type PluginInstanceEventHandler struct {
	*BaseInstanceEventHandler
	subscriber plugin.DiscoverChannel
}

func (p *PluginInstanceEventHandler) OnEvent(ctx context.Context, any2 any) error {
	e := any2.(model.InstanceEvent)
	p.subscriber.PublishEvent(e)
	return nil
}

// 插件初始化
func pluginInitialize() {
	// 获取CMDB插件
	namingServer.cmdb = plugin.GetCMDB()
	if namingServer.cmdb == nil {
		log.Warnf("Not Found CMDB Plugin")
	}

	// 获取History插件，注意：插件的配置在bootstrap已经设置好
	namingServer.history = plugin.GetHistory()
	if namingServer.history == nil {
		log.Warnf("Not Found History Log Plugin")
	}

	// 获取限流插件
	namingServer.ratelimit = plugin.GetRatelimit()
	if namingServer.ratelimit == nil {
		log.Warnf("Not found Ratelimit Plugin")
	}

	subscriber := plugin.GetDiscoverEvent()
	if subscriber == nil {
		log.Warnf("Not found DiscoverEvent Plugin")
		return
	}

	eventHandler := &PluginInstanceEventHandler{
		BaseInstanceEventHandler: NewBaseInstanceEventHandler(namingServer),
		subscriber:               subscriber,
	}
	if err := eventhub.Subscribe(eventhub.InstanceEventTopic, subscriber.Name(), eventHandler); err != nil {
		log.Warnf("register DiscoverEvent into eventhub:%s %v", subscriber.Name(), err)
	}

}
