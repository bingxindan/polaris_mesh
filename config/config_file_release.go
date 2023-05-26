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
	"encoding/base64"
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

// PublishConfigFile 发布配置文件
func (s *Server) PublishConfigFile(
	ctx context.Context, configFileRelease *apiconfig.ConfigFileRelease) *apiconfig.ConfigResponse {
	namespace := configFileRelease.Namespace.GetValue()
	group := configFileRelease.Group.GetValue()
	fileName := configFileRelease.FileName.GetValue()

	if err := utils2.CheckFileName(utils.NewStringValue(fileName)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidConfigFileName, nil)
	}

	if err := utils2.CheckResourceName(utils.NewStringValue(namespace)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidNamespaceName, nil)
	}

	if err := utils2.CheckResourceName(utils.NewStringValue(group)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidConfigFileGroupName, nil)
	}

	if !s.checkNamespaceExisted(namespace) {
		return api.NewConfigFileReleaseResponse(apimodel.Code_NotFoundNamespace, configFileRelease)
	}

	userName := utils.ParseUserName(ctx)
	configFileRelease.CreateBy = utils.NewStringValue(userName)
	configFileRelease.ModifyBy = utils.NewStringValue(userName)

	tx := s.getTx(ctx)
	// 获取待发布的 configFile 信息
	toPublishFile, err := s.storage.GetConfigFile(tx, namespace, group, fileName)

	requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
	if err != nil {
		log.Error("[Config][Service] get config file error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("group", group),
			zap.String("fileName", fileName),
			zap.Error(err))

		s.recordReleaseFail(ctx, transferConfigFileReleaseAPIModel2StoreModel(configFileRelease))

		return api.NewConfigFileResponse(apimodel.Code_StoreLayerException, nil)
	}

	if toPublishFile == nil {
		return api.NewConfigFileResponse(apimodel.Code_NotFoundResource, nil)
	}

	md5 := utils2.CalMd5(toPublishFile.Content)

	// 获取 configFileRelease 信息
	managedFileRelease, err := s.storage.GetConfigFileReleaseWithAllFlag(tx, namespace, group, fileName)
	if err != nil {
		log.Error("[Config][Service] get config file release error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("group", group),
			zap.String("fileName", fileName),
			zap.Error(err))

		s.recordReleaseFail(ctx, transferConfigFileReleaseAPIModel2StoreModel(configFileRelease))

		return api.NewConfigFileResponse(apimodel.Code_StoreLayerException, nil)
	}

	releaseName := configFileRelease.Name.GetValue()
	if releaseName == "" {
		if managedFileRelease == nil {
			releaseName = utils2.GenReleaseName("", fileName)
		} else {
			releaseName = utils2.GenReleaseName(managedFileRelease.Name, fileName)
		}
	}

	// 第一次发布
	if managedFileRelease == nil {
		fileRelease := &model.ConfigFileRelease{
			Name:      releaseName,
			Namespace: namespace,
			Group:     group,
			FileName:  fileName,
			Content:   toPublishFile.Content,
			Comment:   configFileRelease.Comment.GetValue(),
			Md5:       md5,
			Version:   1,
			Flag:      0,
			CreateBy:  configFileRelease.CreateBy.GetValue(),
			ModifyBy:  configFileRelease.CreateBy.GetValue(),
		}

		createdFileRelease, err := s.storage.CreateConfigFileRelease(tx, fileRelease)
		if err != nil {
			log.Error("[Config][Service] create config file release error.",
				utils.ZapRequestID(requestID),
				zap.String("namespace", namespace),
				zap.String("group", group),
				zap.String("fileName", fileName),
				zap.Error(err))

			s.recordReleaseFail(ctx, transferConfigFileReleaseAPIModel2StoreModel(configFileRelease))

			return api.NewConfigFileResponse(apimodel.Code_StoreLayerException, nil)
		}

		s.RecordHistory(ctx, configFileReleaseRecordEntry(ctx, configFileRelease, createdFileRelease, model.OCreate))
		s.recordReleaseHistory(ctx, createdFileRelease, utils.ReleaseTypeNormal, utils.ReleaseStatusSuccess)

		return api.NewConfigFileReleaseResponse(
			apimodel.Code_ExecuteSuccess, configFileRelease2Api(createdFileRelease))
	}

	// 更新发布
	fileRelease := &model.ConfigFileRelease{
		Name:      releaseName,
		Namespace: namespace,
		Group:     group,
		FileName:  fileName,
		Content:   toPublishFile.Content,
		Comment:   configFileRelease.Comment.GetValue(),
		Md5:       md5,
		Version:   managedFileRelease.Version + 1,
		ModifyBy:  configFileRelease.CreateBy.GetValue(),
	}

	updatedFileRelease, err := s.storage.UpdateConfigFileRelease(tx, fileRelease)
	if err != nil {
		log.Error("[Config][Service] update config file release error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("group", group),
			zap.String("fileName", fileName),
			zap.Error(err))

		s.recordReleaseFail(ctx, transferConfigFileReleaseAPIModel2StoreModel(configFileRelease))

		return api.NewConfigFileResponse(apimodel.Code_StoreLayerException, nil)
	}

	s.recordReleaseHistory(ctx, updatedFileRelease, utils.ReleaseTypeNormal, utils.ReleaseStatusSuccess)
	s.RecordHistory(ctx, configFileReleaseRecordEntry(ctx, configFileRelease, updatedFileRelease, model.OCreate))

	return api.NewConfigFileReleaseResponse(apimodel.Code_ExecuteSuccess, configFileRelease2Api(updatedFileRelease))
}

// GetConfigFileRelease 获取配置文件发布内容
func (s *Server) GetConfigFileRelease(
	ctx context.Context, namespace, group, fileName string) *apiconfig.ConfigResponse {
	if err := utils2.CheckFileName(utils.NewStringValue(fileName)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidConfigFileName, nil)
	}

	if err := utils2.CheckResourceName(utils.NewStringValue(namespace)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidNamespaceName, nil)
	}

	if err := utils2.CheckResourceName(utils.NewStringValue(group)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidConfigFileGroupName, nil)
	}

	fileRelease, err := s.storage.GetConfigFileRelease(s.getTx(ctx), namespace, group, fileName)

	if err != nil {
		requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)
		log.Error("[Config][Service]get config file release error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("group", group),
			zap.String("fileName", fileName),
			zap.Error(err))

		return api.NewConfigFileResponse(apimodel.Code_StoreLayerException, nil)
	}

	if fileRelease == nil {
		return api.NewConfigFileReleaseResponse(apimodel.Code_ExecuteSuccess, nil)
	}
	// 解密发布纪录中配置
	release := configFileRelease2Api(fileRelease)
	if err := s.decryptConfigFileRelease(ctx, release); err != nil {
		log.Error("[Config][Service]get config file release error.",
			utils.ZapRequestIDByCtx(ctx),
			zap.String("namespace", namespace),
			zap.String("group", group),
			zap.String("fileName", fileName),
			zap.Error(err))
		return api.NewConfigFileResponse(apimodel.Code_DecryptConfigFileException, nil)
	}
	return api.NewConfigFileReleaseResponse(apimodel.Code_ExecuteSuccess, release)
}

// DeleteConfigFileRelease 删除配置文件发布，删除配置文件的时候，同步删除配置文件发布数据
func (s *Server) DeleteConfigFileRelease(ctx context.Context, namespace,
	group, fileName, deleteBy string) *apiconfig.ConfigResponse {

	if err := utils2.CheckFileName(utils.NewStringValue(fileName)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidConfigFileName, nil)
	}

	if err := utils2.CheckResourceName(utils.NewStringValue(namespace)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidNamespaceName, nil)
	}

	if err := utils2.CheckResourceName(utils.NewStringValue(group)); err != nil {
		return api.NewConfigFileResponse(apimodel.Code_InvalidConfigFileGroupName, nil)
	}

	latestReleaseRsp := s.GetConfigFileRelease(ctx, namespace, group, fileName)
	if latestReleaseRsp.Code.GetValue() != uint32(apimodel.Code_ExecuteSuccess) {
		return api.NewConfigFileResponse(apimodel.Code(latestReleaseRsp.Code.GetValue()), nil)
	}

	requestID, _ := ctx.Value(utils.StringContext("request-id")).(string)

	var releaseName string
	latestRelease := latestReleaseRsp.ConfigFileRelease
	if latestRelease == nil {
		// 从来没有发布过，无需删除
		return api.NewConfigFileResponse(apimodel.Code_ExecuteSuccess, nil)
	}

	releaseName = utils2.GenReleaseName(latestRelease.Name.GetValue(), fileName)
	if releaseName != latestRelease.Name.GetValue() {
		// 更新releaseName
		releaseModel := transferConfigFileReleaseAPIModel2StoreModel(latestRelease)
		releaseModel.Name = releaseName
		_, err := s.storage.UpdateConfigFileRelease(s.getTx(ctx), releaseModel)
		if err != nil {
			log.Error("[Config][Service] update release name error when delete release.",
				utils.ZapRequestID(requestID),
				zap.String("namespace", namespace),
				zap.String("group", group),
				zap.String("fileName", fileName),
				zap.Error(err))
			return api.NewConfigFileResponse(apimodel.Code_StoreLayerException, nil)
		}
	}

	err := s.storage.DeleteConfigFileRelease(s.getTx(ctx), namespace, group, fileName, deleteBy)

	if err != nil {
		log.Error("[Config][Service] delete config file release error.",
			utils.ZapRequestID(requestID),
			zap.String("namespace", namespace),
			zap.String("group", group),
			zap.String("fileName", fileName),
			zap.Error(err))

		s.recordReleaseHistory(ctx, &model.ConfigFileRelease{
			Name:      releaseName,
			Namespace: namespace,
			Group:     group,
			FileName:  fileName,
			ModifyBy:  deleteBy,
		}, utils.ReleaseTypeDelete, utils.ReleaseStatusFail)

		return api.NewConfigFileResponse(apimodel.Code_StoreLayerException, nil)
	}

	data := &model.ConfigFileRelease{
		Name:      releaseName,
		Namespace: namespace,
		Group:     group,
		FileName:  fileName,
		ModifyBy:  deleteBy,
	}
	s.recordReleaseHistory(ctx, data, utils.ReleaseTypeDelete, utils.ReleaseStatusSuccess)
	s.RecordHistory(ctx, configFileReleaseRecordEntry(ctx, &apiconfig.ConfigFileRelease{
		Namespace: utils.NewStringValue(namespace),
		Name:      utils.NewStringValue(releaseName),
		Group:     utils.NewStringValue(group),
		FileName:  utils.NewStringValue(fileName),
	}, data, model.ODelete))

	return api.NewConfigFileReleaseResponse(apimodel.Code_ExecuteSuccess, nil)
}

func (s *Server) recordReleaseFail(ctx context.Context, configFileRelease *model.ConfigFileRelease) {
	s.recordReleaseHistory(ctx, configFileRelease, utils.ReleaseTypeNormal, utils.ReleaseStatusFail)
}

func transferConfigFileReleaseAPIModel2StoreModel(release *apiconfig.ConfigFileRelease) *model.ConfigFileRelease {
	if release == nil {
		return nil
	}
	var comment string
	if release.Comment != nil {
		comment = release.Comment.Value
	}
	var content string
	if release.Content != nil {
		content = release.Content.Value
	}
	var md5 string
	if release.Md5 != nil {
		md5 = release.Md5.Value
	}
	var version uint64
	if release.Version != nil {
		version = release.Version.Value
	}
	var createBy string
	if release.CreateBy != nil {
		createBy = release.CreateBy.Value
	}
	var modifyBy string
	if release.ModifyBy != nil {
		createBy = release.ModifyBy.Value
	}
	var id uint64
	if release.Id != nil {
		id = release.Id.Value
	}

	return &model.ConfigFileRelease{
		Id:        id,
		Namespace: release.Namespace.GetValue(),
		Group:     release.Group.GetValue(),
		FileName:  release.FileName.GetValue(),
		Content:   content,
		Comment:   comment,
		Md5:       md5,
		Version:   version,
		CreateBy:  createBy,
		ModifyBy:  modifyBy,
	}
}

func configFileRelease2Api(release *model.ConfigFileRelease) *apiconfig.ConfigFileRelease {
	if release == nil {
		return nil
	}

	return &apiconfig.ConfigFileRelease{
		Id:         utils.NewUInt64Value(release.Id),
		Name:       utils.NewStringValue(release.Name),
		Namespace:  utils.NewStringValue(release.Namespace),
		Group:      utils.NewStringValue(release.Group),
		FileName:   utils.NewStringValue(release.FileName),
		Content:    utils.NewStringValue(release.Content),
		Comment:    utils.NewStringValue(release.Comment),
		Md5:        utils.NewStringValue(release.Md5),
		Version:    utils.NewUInt64Value(release.Version),
		CreateBy:   utils.NewStringValue(release.CreateBy),
		CreateTime: utils.NewStringValue(commontime.Time2String(release.CreateTime)),
		ModifyBy:   utils.NewStringValue(release.ModifyBy),
		ModifyTime: utils.NewStringValue(commontime.Time2String(release.ModifyTime)),
	}
}

// configFileReleaseRecordEntry 生成服务的记录entry
func configFileReleaseRecordEntry(ctx context.Context, req *apiconfig.ConfigFileRelease, md *model.ConfigFileRelease,
	operationType model.OperationType) *model.RecordEntry {

	marshaler := jsonpb.Marshaler{}
	detail, _ := marshaler.MarshalToString(req)

	entry := &model.RecordEntry{
		ResourceType:  model.RConfigFileRelease,
		ResourceName:  req.GetName().GetValue(),
		Namespace:     req.GetNamespace().GetValue(),
		OperationType: operationType,
		Operator:      utils.ParseOperator(ctx),
		Detail:        detail,
		HappenTime:    time.Now(),
	}

	return entry
}

// decryptConfigFileRelease 解密配置文件发布纪录
func (s *Server) decryptConfigFileRelease(ctx context.Context, release *apiconfig.ConfigFileRelease) error {
	if s.cryptoManager == nil || release == nil {
		return nil
	}
	// 非创建人请求不解密
	if utils.ParseUserName(ctx) != release.CreateBy.GetValue() {
		return nil
	}
	algorithm, dataKey, err := s.getEncryptAlgorithmAndDataKey(ctx, release.GetNamespace().GetValue(),
		release.GetGroup().GetValue(), release.GetName().GetValue())
	if err != nil {
		return err
	}
	if dataKey == "" {
		return nil
	}
	dateKeyBytes, err := base64.StdEncoding.DecodeString(dataKey)
	if err != nil {
		return err
	}
	crypto, err := s.cryptoManager.GetCrypto(algorithm)
	if err != nil {
		return err
	}

	plainContent, err := crypto.Decrypt(release.Content.GetValue(), dateKeyBytes)
	if err != nil {
		return err
	}
	release.Content = utils.NewStringValue(plainContent)
	return nil
}
