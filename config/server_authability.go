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
	"strconv"

	apiconfig "github.com/polarismesh/specification/source/go/api/v1/config_manage"
	apisecurity "github.com/polarismesh/specification/source/go/api/v1/security"
	"go.uber.org/zap"

	"github.com/polarismesh/polaris/auth"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/common/utils"
)

var _ ConfigCenterServer = (*serverAuthability)(nil)

// Server 配置中心核心服务
type serverAuthability struct {
	targetServer *Server
	userMgn      auth.UserServer
	strategyMgn  auth.StrategyServer
}

func newServerAuthAbility(targetServer *Server,
	userMgn auth.UserServer, strategyMgn auth.StrategyServer) ConfigCenterServer {
	proxy := &serverAuthability{
		targetServer: targetServer,
		userMgn:      userMgn,
		strategyMgn:  strategyMgn,
	}
	targetServer.SetResourceHooks(proxy)
	return proxy
}

func (s *serverAuthability) collectConfigFileAuthContext(ctx context.Context, req []*apiconfig.ConfigFile,
	op model.ResourceOperation, methodName string) *model.AcquireContext {
	return model.NewAcquireContext(
		model.WithRequestContext(ctx),
		model.WithModule(model.ConfigModule),
		model.WithOperation(op),
		model.WithMethod(methodName),
		model.WithAccessResources(s.queryConfigFileResource(ctx, req)),
	)
}

func (s *serverAuthability) collectClientConfigFileAuthContext(ctx context.Context, req []*apiconfig.ConfigFile,
	op model.ResourceOperation, methodName string) *model.AcquireContext {
	return model.NewAcquireContext(
		model.WithRequestContext(ctx),
		model.WithModule(model.ConfigModule),
		model.WithOperation(op),
		model.WithMethod(methodName),
		model.WithFromClient(),
		model.WithAccessResources(s.queryConfigFileResource(ctx, req)),
	)
}

func (s *serverAuthability) collectConfigFileReleaseAuthContext(ctx context.Context, req []*apiconfig.ConfigFileRelease,
	op model.ResourceOperation, methodName string) *model.AcquireContext {
	return model.NewAcquireContext(
		model.WithRequestContext(ctx),
		model.WithModule(model.ConfigModule),
		model.WithOperation(op),
		model.WithMethod(methodName),
		model.WithAccessResources(s.queryConfigFileReleaseResource(ctx, req)),
	)
}

func (s *serverAuthability) collectClientConfigFileReleaseAuthContext(ctx context.Context,
	req []*apiconfig.ConfigFileRelease, op model.ResourceOperation, methodName string) *model.AcquireContext {
	return model.NewAcquireContext(
		model.WithRequestContext(ctx),
		model.WithModule(model.ConfigModule),
		model.WithOperation(op),
		model.WithMethod(methodName),
		model.WithFromClient(),
		model.WithAccessResources(s.queryConfigFileReleaseResource(ctx, req)),
	)
}

func (s *serverAuthability) collectConfigFileReleaseHistoryAuthContext(
	ctx context.Context,
	req []*apiconfig.ConfigFileReleaseHistory,
	op model.ResourceOperation, methodName string) *model.AcquireContext {
	return model.NewAcquireContext(
		model.WithRequestContext(ctx),
		model.WithModule(model.ConfigModule),
		model.WithOperation(op),
		model.WithMethod(methodName),
		model.WithAccessResources(s.queryConfigFileReleaseHistoryResource(ctx, req)),
	)
}

func (s *serverAuthability) collectConfigGroupAuthContext(ctx context.Context, req []*apiconfig.ConfigFileGroup,
	op model.ResourceOperation, methodName string) *model.AcquireContext {
	return model.NewAcquireContext(
		model.WithRequestContext(ctx),
		model.WithModule(model.ConfigModule),
		model.WithOperation(op),
		model.WithMethod(methodName),
		model.WithAccessResources(s.queryConfigGroupResource(ctx, req)),
	)
}

func (s *serverAuthability) collectConfigFileTemplateAuthContext(ctx context.Context,
	req []*apiconfig.ConfigFileTemplate, op model.ResourceOperation, methodName string) *model.AcquireContext {
	return model.NewAcquireContext(
		model.WithRequestContext(ctx),
		model.WithModule(model.ConfigModule),
	)
}

func (s *serverAuthability) queryConfigGroupResource(ctx context.Context,
	req []*apiconfig.ConfigFileGroup) map[apisecurity.ResourceType][]model.ResourceEntry {

	names := utils.NewStringSet()
	namespace := req[0].GetNamespace().GetValue()
	for index := range req {
		names.Add(req[index].GetName().GetValue())
	}
	entries, err := s.queryConfigGroupRsEntryByNames(ctx, namespace, names.ToSlice())
	if err != nil {
		authLog.Error("[Config][Server] collect config_file_group res",
			utils.ZapRequestIDByCtx(ctx), zap.Error(err))
		return nil
	}
	ret := map[apisecurity.ResourceType][]model.ResourceEntry{
		apisecurity.ResourceType_ConfigGroups: entries,
	}
	authLog.Debug("[Config][Server] collect config_file_group access res",
		utils.ZapRequestIDByCtx(ctx), zap.Any("res", ret))
	return ret
}

// queryConfigFileResource config file资源的鉴权转换为config group的鉴权
func (s *serverAuthability) queryConfigFileResource(ctx context.Context,
	req []*apiconfig.ConfigFile) map[apisecurity.ResourceType][]model.ResourceEntry {

	if len(req) == 0 {
		return nil
	}
	namespace := req[0].Namespace.GetValue()
	groupNames := utils.NewStringSet()

	for _, apiConfigFile := range req {
		groupNames.Add(apiConfigFile.Group.GetValue())
	}
	entries, err := s.queryConfigGroupRsEntryByNames(ctx, namespace, groupNames.ToSlice())
	if err != nil {
		authLog.Error("[Config][Server] collect config_file res",
			utils.ZapRequestIDByCtx(ctx), zap.Error(err))
		return nil
	}
	ret := map[apisecurity.ResourceType][]model.ResourceEntry{
		apisecurity.ResourceType_ConfigGroups: entries,
	}
	authLog.Debug("[Config][Server] collect config_file access res",
		utils.ZapRequestIDByCtx(ctx), zap.Any("res", ret))
	return ret
}

func (s *serverAuthability) queryConfigFileReleaseResource(ctx context.Context,
	req []*apiconfig.ConfigFileRelease) map[apisecurity.ResourceType][]model.ResourceEntry {

	if len(req) == 0 {
		return nil
	}
	namespace := req[0].Namespace.GetValue()
	groupNames := utils.NewStringSet()

	for _, apiConfigFile := range req {
		groupNames.Add(apiConfigFile.Group.GetValue())
	}
	entries, err := s.queryConfigGroupRsEntryByNames(ctx, namespace, groupNames.ToSlice())
	if err != nil {
		authLog.Debug("[Config][Server] collect config_file res",
			utils.ZapRequestIDByCtx(ctx), zap.Error(err))
		return nil
	}
	ret := map[apisecurity.ResourceType][]model.ResourceEntry{
		apisecurity.ResourceType_ConfigGroups: entries,
	}
	authLog.Debug("[Config][Server] collect config_file access res",
		utils.ZapRequestIDByCtx(ctx), zap.Any("res", ret))
	return ret
}

func (s *serverAuthability) queryConfigFileReleaseHistoryResource(ctx context.Context,
	req []*apiconfig.ConfigFileReleaseHistory) map[apisecurity.ResourceType][]model.ResourceEntry {

	if len(req) == 0 {
		return nil
	}
	namespace := req[0].Namespace.GetValue()
	groupNames := utils.NewStringSet()

	for _, apiConfigFile := range req {
		groupNames.Add(apiConfigFile.Group.GetValue())
	}
	entries, err := s.queryConfigGroupRsEntryByNames(ctx, namespace, groupNames.ToSlice())
	if err != nil {
		authLog.Debug("[Config][Server] collect config_file res",
			utils.ZapRequestIDByCtx(ctx), zap.Error(err))
		return nil
	}
	ret := map[apisecurity.ResourceType][]model.ResourceEntry{
		apisecurity.ResourceType_ConfigGroups: entries,
	}
	authLog.Debug("[Config][Server] collect config_file access res",
		utils.ZapRequestIDByCtx(ctx), zap.Any("res", ret))
	return ret
}

func (s *serverAuthability) queryConfigGroupRsEntryByNames(ctx context.Context, namespace string,
	names []string) ([]model.ResourceEntry, error) {

	configFileGroups := make([]*model.ConfigFileGroup, 0, len(names))
	for i := range names {
		data, err := s.targetServer.fileCache.GetOrLoadGroupByName(namespace, names[i])
		if err != nil {
			return nil, err
		}

		if data == nil {
			continue
		}

		configFileGroups = append(configFileGroups, data)
	}

	entries := make([]model.ResourceEntry, 0, len(configFileGroups))

	for index := range configFileGroups {
		group := configFileGroups[index]
		entries = append(entries, model.ResourceEntry{
			ID:    strconv.FormatUint(group.Id, 10),
			Owner: group.Owner,
		})
	}
	return entries, nil
}
