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
	"sort"
	"strings"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	apiconfig "github.com/polarismesh/specification/source/go/api/v1/config_manage"
	apimodel "github.com/polarismesh/specification/source/go/api/v1/model"
	"go.uber.org/zap"

	api "github.com/polarismesh/polaris/common/api/v1"
	"github.com/polarismesh/polaris/common/model"
	commontime "github.com/polarismesh/polaris/common/time"
	"github.com/polarismesh/polaris/common/utils"
	utils2 "github.com/polarismesh/polaris/config/utils"
)

// CreateConfigFileGroup 创建配置文件组
func (s *Server) CreateConfigFileGroup(
	ctx context.Context, configFileGroup *apiconfig.ConfigFileGroup) *apiconfig.ConfigResponse {
	requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
	if checkError := checkConfigFileGroupParams(configFileGroup); checkError != nil {
		return checkError
	}

	userName := utils.ParseUserName(ctx)
	configFileGroup.CreateBy = utils.NewStringValue(userName)
	configFileGroup.ModifyBy = utils.NewStringValue(userName)

	namespace := configFileGroup.Namespace.GetValue()
	groupName := configFileGroup.Name.GetValue()

	// 如果 namespace 不存在则自动创建
	_, errResp := s.namespaceOperator.CreateNamespaceIfAbsent(ctx, &apimodel.Namespace{
		Name: utils.NewStringValue(namespace),
	})
	if errResp != nil {
		log.Error("[Config][Service] create namespace failed.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("group", groupName),
			zap.String("err", errResp.String()))
		return api.NewConfigFileGroupResponse(apimodel.Code(errResp.Code.GetValue()), configFileGroup)
	}

	fileGroup, err := s.storage.GetConfigFileGroup(namespace, groupName)
	if err != nil {
		log.Error("[Config][Service] get config file group error.",
			utils.ZapRequestID(requestID),
			zap.Error(err))
		return api.NewConfigFileGroupResponse(apimodel.Code_StoreLayerException, configFileGroup)
	}

	if fileGroup != nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_ExistedResource, configFileGroup)
	}

	toCreateGroup := apiConfigFileGroup2Model(configFileGroup)
	toCreateGroup.ModifyBy = toCreateGroup.CreateBy

	createdGroup, err := s.storage.CreateConfigFileGroup(toCreateGroup)
	if err != nil {
		log.Error("[Config][Service] create config file group error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("groupName", groupName),
			zap.Error(err))
		return api.NewConfigFileGroupResponse(apimodel.Code_StoreLayerException, configFileGroup)
	}

	log.Info("[Config][Service] create config file group successful.",
		utils.ZapRequestID(requestID),
		zap.String("namespace", namespace),
		zap.String("groupName", groupName))

	// 这里设置在 config-group 的 id 信息
	configFileGroup.Id = utils.NewUInt64Value(createdGroup.Id)
	if err := s.afterConfigGroupResource(ctx, configFileGroup); err != nil {
		log.Error("[Config][Service] create config_file_group after resource",
			utils.ZapRequestIDByCtx(ctx), zap.Error(err))
		return api.NewConfigFileGroupResponse(apimodel.Code_ExecuteException, nil)
	}

	s.RecordHistory(ctx, configGroupRecordEntry(ctx, configFileGroup, createdGroup, model.OCreate))

	return api.NewConfigFileGroupResponse(apimodel.Code_ExecuteSuccess, configFileGroup2Api(createdGroup))
}

// createConfigFileGroupIfAbsent 如果不存在配置文件组，则自动创建
func (s *Server) createConfigFileGroupIfAbsent(ctx context.Context,
	configFileGroup *apiconfig.ConfigFileGroup) *apiconfig.ConfigResponse {
	var (
		namespace  = configFileGroup.Namespace.GetValue()
		name       = configFileGroup.Name.GetValue()
		group, err = s.storage.GetConfigFileGroup(namespace, name)
	)

	if err != nil {
		requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
		log.Error("[Config][Service] query config file group error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("groupName", name),
			zap.Error(err))

		return api.NewConfigFileGroupResponse(apimodel.Code_StoreLayerException, nil)
	}

	if group != nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_ExecuteSuccess, configFileGroup2Api(group))
	}

	return s.CreateConfigFileGroup(ctx, configFileGroup)
}

// QueryConfigFileGroups 查询配置文件组
func (s *Server) QueryConfigFileGroups(ctx context.Context, namespace, groupName,
	fileName string, offset, limit uint32) *apiconfig.ConfigBatchQueryResponse {
	if limit > MaxPageSize {
		return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_InvalidParameter, 0, nil)
	}

	// 按分组名搜索
	if fileName == "" {
		return s.queryByGroupName(ctx, namespace, groupName, offset, limit)
	}

	// 按文件搜索
	return s.queryByFileName(ctx, namespace, groupName, fileName, offset, limit)
}

func (s *Server) queryByGroupName(ctx context.Context, namespace, groupName string,
	offset, limit uint32) *apiconfig.ConfigBatchQueryResponse {
	count, groups, err := s.storage.QueryConfigFileGroups(namespace, groupName, offset, limit)
	if err != nil {
		requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
		log.Error("[Config][Service] query config file group error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("groupName", groupName),
			zap.Error(err))

		return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_StoreLayerException, 0, nil)
	}

	if len(groups) == 0 {
		return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_ExecuteSuccess, count, nil)
	}

	groupAPIModels, err := s.batchTransfer(ctx, groups)
	if err != nil {
		return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_StoreLayerException, 0, nil)
	}

	return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_ExecuteSuccess, count, groupAPIModels)
}

func (s *Server) queryByFileName(ctx context.Context, namespace, groupName,
	fileName string, offset uint32, limit uint32) *apiconfig.ConfigBatchQueryResponse {
	// 内存分页，先获取到所有配置文件
	rsp := s.queryConfigFileWithoutTags(ctx, namespace, groupName, fileName, 0, 10000)
	if rsp.Code.GetValue() != uint32(apimodel.Code_ExecuteSuccess) {
		return rsp
	}

	// 获取所有的 group 信息
	groupMap := make(map[string]bool)
	for _, configFile := range rsp.ConfigFiles {
		// namespace+group 是唯一键
		groupMap[configFile.Namespace.Value+"+"+configFile.Group.Value] = true
	}

	if len(groupMap) == 0 {
		return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_ExecuteSuccess, 0, nil)
	}

	var distinctGroupNames []string
	for key := range groupMap {
		distinctGroupNames = append(distinctGroupNames, key)
	}

	// 按 groupName 字典排序
	sort.Strings(distinctGroupNames)

	// 分页
	total := len(distinctGroupNames)
	if int(offset) >= total {
		return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_ExecuteSuccess, uint32(total), nil)
	}

	var pageGroupNames []string
	if int(offset+limit) >= total {
		pageGroupNames = distinctGroupNames[offset:total]
	} else {
		pageGroupNames = distinctGroupNames[offset : offset+limit]
	}

	// 渲染
	var configFileGroups []*model.ConfigFileGroup
	for _, pageGroupName := range pageGroupNames {
		namespaceAndGroup := strings.Split(pageGroupName, "+")
		configFileGroup, err := s.storage.GetConfigFileGroup(namespaceAndGroup[0], namespaceAndGroup[1])
		if err != nil {
			requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
			log.Error("[Config][Service] get config file group error.",
				utils.ZapRequestID(requestID),
				zap.String("namespace", namespaceAndGroup[0]),
				zap.String("name", namespaceAndGroup[1]),
				zap.Error(err))
			return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_StoreLayerException, 0, nil)
		}
		configFileGroups = append(configFileGroups, configFileGroup)
	}

	groupAPIModels, err := s.batchTransfer(ctx, configFileGroups)
	if err != nil {
		return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_StoreLayerException, 0, nil)
	}

	return api.NewConfigFileGroupBatchQueryResponse(apimodel.Code_ExecuteSuccess, uint32(total), groupAPIModels)
}

func (s *Server) batchTransfer(
	ctx context.Context, groups []*model.ConfigFileGroup) ([]*apiconfig.ConfigFileGroup, error) {
	var result []*apiconfig.ConfigFileGroup
	for _, groupStoreModel := range groups {
		configFileGroup := configFileGroup2Api(groupStoreModel)
		// enrich config file count
		fileCount, err := s.storage.CountByConfigFileGroup(groupStoreModel.Namespace, groupStoreModel.Name)
		if err != nil {
			requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
			log.Error("[Config][Service] get config file count for group error.",
				utils.ZapRequestID(requestID),
				zap.String("namespace", groupStoreModel.Namespace),
				zap.String("groupName", groupStoreModel.Name),
				zap.Error(err))
			return nil, err
		}
		configFileGroup.FileCount = utils.NewUInt64Value(fileCount)

		result = append(result, configFileGroup)
	}
	return result, nil
}

// DeleteConfigFileGroup 删除配置文件组
func (s *Server) DeleteConfigFileGroup(ctx context.Context, namespace, name string) *apiconfig.ConfigResponse {
	if err := utils2.CheckResourceName(utils.NewStringValue(namespace)); err != nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_InvalidNamespaceName, nil)
	}
	if err := utils2.CheckResourceName(utils.NewStringValue(name)); err != nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_InvalidConfigFileGroupName, nil)
	}

	operator := utils.ParseUserName(ctx)

	requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
	log.Info("[Config][Service] delete config file group. ",
		utils.ZapRequestID(requestID),
		zap.String("namespace", namespace),
		zap.String("name", name))

	// 删除配置文件组，同时删除组下面所有的配置文件
	startOffset := uint32(0)
	hasMore := true
	for hasMore {
		queryRsp := s.QueryConfigFilesByGroup(ctx, namespace, name, startOffset, MaxPageSize)
		if queryRsp.Code.GetValue() != uint32(apimodel.Code_ExecuteSuccess) {
			log.Error("[Config][Service] get group's config file failed. ",
				utils.ZapRequestID(requestID),
				zap.String("namespace", namespace),
				zap.String("name", name))
			return api.NewConfigFileGroupResponse(apimodel.Code(queryRsp.Code.GetValue()), nil)
		}
		configFiles := queryRsp.ConfigFiles

		deleteRsp := s.BatchDeleteConfigFile(ctx, configFiles, operator)
		if deleteRsp.Code.GetValue() != uint32(apimodel.Code_ExecuteSuccess) {
			log.Error("[Config][Service] batch delete group's config file failed. ",
				utils.ZapRequestID(requestID),
				zap.String("namespace", namespace),
				zap.String("name", name))
			return api.NewConfigFileGroupResponse(apimodel.Code(deleteRsp.Code.GetValue()), nil)
		}

		if hasMore = len(queryRsp.ConfigFiles) >= MaxPageSize; hasMore {
			startOffset += MaxPageSize
		}
	}

	configGroup, err := s.storage.GetConfigFileGroup(namespace, name)
	if err != nil {
		log.Error("[Config][Service] get config file group failed. ",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("name", name),
			zap.Error(err))

		return api.NewConfigFileGroupResponse(apimodel.Code_StoreLayerException, nil)
	}
	if configGroup == nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_NotFoundResource, nil)
	}

	if err := s.storage.DeleteConfigFileGroup(namespace, name); err != nil {
		log.Error("[Config][Service] delete config file group failed. ",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("name", name),
			zap.Error(err))

		return api.NewConfigFileGroupResponse(apimodel.Code_StoreLayerException, nil)
	}

	if err := s.afterConfigGroupResource(ctx, &apiconfig.ConfigFileGroup{
		Id:        utils.NewUInt64Value(configGroup.Id),
		Namespace: utils.NewStringValue(configGroup.Namespace),
		Name:      utils.NewStringValue(configGroup.Name),
	}); err != nil {
		log.Error("[Config][Service] delete config_file_group after resource",
			utils.ZapRequestIDByCtx(ctx), zap.Error(err))
		return api.NewConfigFileGroupResponse(apimodel.Code_ExecuteException, nil)
	}
	s.RecordHistory(ctx, configGroupRecordEntry(ctx, &apiconfig.ConfigFileGroup{
		Namespace: utils.NewStringValue(namespace),
		Name:      utils.NewStringValue(name),
	}, configGroup, model.ODelete))

	return api.NewConfigFileGroupResponse(apimodel.Code_ExecuteSuccess, nil)
}

// UpdateConfigFileGroup 更新配置文件组
func (s *Server) UpdateConfigFileGroup(ctx context.Context,
	configFileGroup *apiconfig.ConfigFileGroup) *apiconfig.ConfigResponse {
	if resp := checkConfigFileGroupParams(configFileGroup); resp != nil {
		return resp
	}

	namespace := configFileGroup.Namespace.GetValue()
	groupName := configFileGroup.Name.GetValue()

	fileGroup, err := s.storage.GetConfigFileGroup(namespace, groupName)

	if err != nil {
		log.Error("[Config][Service] get config file group failed. ",
			utils.ZapRequestID(utils.ParseRequestID(ctx)),
			zap.String("namespace", namespace),
			zap.String("name", groupName),
			zap.Error(err))

		return api.NewConfigFileGroupResponse(apimodel.Code_StoreLayerException, configFileGroup)
	}

	if fileGroup == nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_NotFoundResource, configFileGroup)
	}

	userName := utils.ParseUserName(ctx)
	configFileGroup.ModifyBy = utils.NewStringValue(userName)

	toUpdateGroup := apiConfigFileGroup2Model(configFileGroup)
	toUpdateGroup.ModifyBy = configFileGroup.ModifyBy.GetValue()

	updatedGroup, err := s.storage.UpdateConfigFileGroup(toUpdateGroup)
	if err != nil {
		log.Error("[Config][Service] update config file group failed. ",
			utils.ZapRequestID(utils.ParseRequestID(ctx)),
			zap.String("namespace", namespace),
			zap.String("name", groupName),
			zap.Error(err))

		return api.NewConfigFileGroupResponse(apimodel.Code_StoreLayerException, configFileGroup)
	}

	configFileGroup.Id = utils.NewUInt64Value(fileGroup.Id)
	if err := s.afterConfigGroupResource(ctx, configFileGroup); err != nil {
		log.Error("[Config][Service] update config_file_group after resource",
			utils.ZapRequestIDByCtx(ctx), zap.Error(err))
		return api.NewConfigFileGroupResponse(apimodel.Code_ExecuteException, nil)
	}

	s.RecordHistory(ctx, configGroupRecordEntry(ctx, configFileGroup, fileGroup, model.OUpdate))

	return api.NewConfigFileGroupResponse(apimodel.Code_ExecuteSuccess, configFileGroup2Api(updatedGroup))
}

func checkConfigFileGroupParams(configFileGroup *apiconfig.ConfigFileGroup) *apiconfig.ConfigResponse {
	if configFileGroup == nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_InvalidParameter, configFileGroup)
	}

	if err := utils2.CheckResourceName(configFileGroup.Name); err != nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_InvalidConfigFileGroupName, configFileGroup)
	}

	if err := utils2.CheckResourceName(configFileGroup.Namespace); err != nil {
		return api.NewConfigFileGroupResponse(apimodel.Code_InvalidNamespaceName, configFileGroup)
	}

	return nil
}

func apiConfigFileGroup2Model(group *apiconfig.ConfigFileGroup) *model.ConfigFileGroup {
	var comment string
	if group.Comment != nil {
		comment = group.Comment.Value
	}
	var createBy string
	if group.CreateBy != nil {
		createBy = group.CreateBy.Value
	}
	var groupOwner string
	if group.Owner != nil && group.Owner.GetValue() != "" {
		groupOwner = group.Owner.GetValue()
	} else {
		groupOwner = createBy
	}
	return &model.ConfigFileGroup{
		Name:      group.Name.GetValue(),
		Namespace: group.Namespace.GetValue(),
		Comment:   comment,
		CreateBy:  createBy,
		Valid:     true,
		Owner:     groupOwner,
	}
}

func configFileGroup2Api(group *model.ConfigFileGroup) *apiconfig.ConfigFileGroup {
	if group == nil {
		return nil
	}
	return &apiconfig.ConfigFileGroup{
		Id:         utils.NewUInt64Value(group.Id),
		Name:       utils.NewStringValue(group.Name),
		Namespace:  utils.NewStringValue(group.Namespace),
		Comment:    utils.NewStringValue(group.Comment),
		Owner:      utils.NewStringValue(group.Owner),
		CreateBy:   utils.NewStringValue(group.CreateBy),
		ModifyBy:   utils.NewStringValue(group.ModifyBy),
		CreateTime: utils.NewStringValue(commontime.Time2String(group.CreateTime)),
		ModifyTime: utils.NewStringValue(commontime.Time2String(group.ModifyTime)),
	}
}

// configGroupRecordEntry 生成服务的记录entry
func configGroupRecordEntry(ctx context.Context, req *apiconfig.ConfigFileGroup, md *model.ConfigFileGroup,
	operationType model.OperationType) *model.RecordEntry {

	marshaler := jsonpb.Marshaler{}
	detail, _ := marshaler.MarshalToString(req)

	entry := &model.RecordEntry{
		ResourceType:  model.RConfigGroup,
		ResourceName:  req.GetName().GetValue(),
		Namespace:     req.GetNamespace().GetValue(),
		OperationType: operationType,
		Operator:      utils.ParseOperator(ctx),
		Detail:        detail,
		HappenTime:    time.Now(),
	}

	return entry
}
