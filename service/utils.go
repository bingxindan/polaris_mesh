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
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/wrappers"
	apiservice "github.com/polarismesh/specification/source/go/api/v1/service_manage"

	api "github.com/polarismesh/polaris/common/api/v1"
	"github.com/polarismesh/polaris/common/utils"
	"github.com/polarismesh/polaris/service/batch"
	"github.com/polarismesh/polaris/store"
)

// some options config
const (
	// QueryDefaultOffset default query offset
	QueryDefaultOffset = 0
	// QueryDefaultLimit default query limit
	QueryDefaultLimit = 100
	// QueryMaxLimit default query max
	QueryMaxLimit = 100

	// MaxMetadataLength metadata max length
	MaxMetadataLength = 64

	MaxBusinessLength   = 64
	MaxOwnersLength     = 1024
	MaxDepartmentLength = 1024
	MaxCommentLength    = 1024

	// service表
	MaxDbServiceNameLength      = 128
	MaxDbServiceNamespaceLength = 64
	MaxDbServicePortsLength     = 8192
	MaxDbServiceBusinessLength  = 128
	MaxDbServiceDeptLength      = 1024
	MaxDbServiceCMDBLength      = 1024
	MaxDbServiceCommentLength   = 1024
	MaxDbServiceOwnerLength     = 1024
	MaxDbServiceToken           = 2048

	// instance表
	MaxDbInsHostLength     = 128
	MaxDbInsProtocolLength = 32
	MaxDbInsVersionLength  = 32
	MaxDbInsLogicSetLength = 128

	// circuitbreaker表
	MaxDbCircuitbreakerName       = 128
	MaxDbCircuitbreakerNamespace  = 64
	MaxDbCircuitbreakerBusiness   = 64
	MaxDbCircuitbreakerDepartment = 1024
	MaxDbCircuitbreakerComment    = 1024
	MaxDbCircuitbreakerOwner      = 1024
	MaxDbCircuitbreakerVersion    = 32

	// platform表
	MaxPlatformIDLength     = 32
	MaxPlatformNameLength   = 128
	MaxPlatformDomainLength = 1024
	MaxPlatformQPS          = 65535

	MaxRuleName = 64

	// ratelimit表
	MaxDbRateLimitName = MaxRuleName

	// MaxDbRoutingName routing_config_v2 表
	MaxDbRoutingName = MaxRuleName

	// ContextDiscoverParam key for discover parameters in context
	ContextDiscoverParam = utils.StringContext("discover-param")

	// ParamKeyInstanceId key for parameter key instanceId
	ParamKeyInstanceId = "instanceId"
)

// checkResourceName 检查资源Name
var resourceNameRE = regexp.MustCompile("^[0-9A-Za-z-./:_]+$")

func checkResourceName(name *wrappers.StringValue) error {
	if name == nil {
		return errors.New(utils.NilErrString)
	}

	if len(name.GetValue()) == 0 {
		return errors.New(utils.EmptyErrString)
	}

	ok := resourceNameRE.MatchString(name.GetValue())
	if !ok {
		return errors.New("name contains invalid character")
	}

	return nil
}

// checkInstanceHost 检查服务实例Host
func checkInstanceHost(host *wrappers.StringValue) error {
	if host == nil {
		return errors.New(utils.NilErrString)
	}

	if host.GetValue() == "" {
		return errors.New(utils.EmptyErrString)
	}

	return nil
}

// checkMetadata 检查metadata的个数; 最大是64个
// key/value是否符合要求
func checkMetadata(meta map[string]string) error {
	if meta == nil {
		return nil
	}

	if len(meta) > MaxMetadataLength {
		return errors.New("metadata is too long")
	}

	/*regStr := "^[0-9A-Za-z-._*]+$"
	  matchFunc := func(str string) error {
	  	if str == "" {
	  		return nil
	  	}
	  	ok, err := regexp.MatchString(regStr, str)
	  	if err != nil {
	  		log.Errorf("regexp match string(%s) err: %s", str, err.Error())
	  		return err
	  	}
	  	if !ok {
	  		log.Errorf("metadata string(%s) contains invalid character", str)
	  		return errors.New("contain invalid character")
	  	}
	  	return nil
	  }
	  for key, value := range meta {
	  	if err := matchFunc(key); err != nil {
	  		return err
	  	}
	  	if err := matchFunc(value); err != nil {
	  		return err
	  	}
	  }*/

	return nil
}

// storeError2Response store code
func storeError2Response(err error) *apiservice.Response {
	if err == nil {
		return nil
	}
	return api.NewResponseWithMsg(batch.StoreCode2APICode(err), err.Error())
}

// storeError2AnyResponse store code
func storeError2AnyResponse(err error, msg proto.Message) *apiservice.Response {
	if err == nil {
		return nil
	}
	if nil == msg {
		return api.NewResponseWithMsg(batch.StoreCode2APICode(err), err.Error())
	}
	resp := api.NewAnyDataResponse(batch.StoreCode2APICode(err), msg)
	resp.Info = &wrappers.StringValue{Value: err.Error()}
	return resp
}

// ParseInstanceArgs 解析服务实例的 ip 和 port 查询参数
func ParseInstanceArgs(query map[string]string, meta map[string]string) (*store.InstanceArgs, error) {
	if len(query) == 0 && meta == nil {
		return nil, nil
	}
	res := &store.InstanceArgs{}
	res.Meta = meta
	if len(query) == 0 {
		return res, nil
	}
	hosts, ok := query["host"]
	if !ok {
		return nil, fmt.Errorf("port parameter can not be used alone without host")
	}
	res.Hosts = strings.Split(hosts, ",")
	ports, ok := query["port"]
	if !ok {
		return res, nil
	}

	portSlices := strings.Split(ports, ",")
	for _, portStr := range portSlices {
		port, err := strconv.ParseUint(portStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%s can not parse as uint, err is %s", portStr, err.Error())
		}
		res.Ports = append(res.Ports, uint32(port))
	}
	return res, nil
}