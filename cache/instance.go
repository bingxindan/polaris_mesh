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

package cache

import (
	"sync"
	"time"

	apimodel "github.com/polarismesh/specification/source/go/api/v1/model"
	apiservice "github.com/polarismesh/specification/source/go/api/v1/service_manage"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
)

const (
	// InstanceName instance name
	InstanceName = "instance"
	// 定时全量对账
	checkAllIntervalSec = 60
)

// InstanceIterProc instance iter proc func
type InstanceIterProc func(key string, value *model.Instance) (bool, error)

// InstanceCache 实例相关的缓存接口
type InstanceCache interface {
	// Cache 公共缓存接口
	Cache
	// GetInstance 根据实例ID获取实例数据
	GetInstance(instanceID string) *model.Instance
	// GetInstancesByServiceID 根据服务名获取实例，先查找服务名对应的服务ID，再找实例列表
	GetInstancesByServiceID(serviceID string) []*model.Instance
	// IteratorInstances 迭代
	IteratorInstances(iterProc InstanceIterProc) error
	// IteratorInstancesWithService 根据服务ID进行迭代
	IteratorInstancesWithService(serviceID string, iterProc InstanceIterProc) error
	// GetInstancesCount 获取instance的个数
	GetInstancesCount() int
	// GetInstancesCountByServiceID 根据服务ID获取实例数
	GetInstancesCountByServiceID(serviceID string) model.InstanceCount
	// GetServicePorts 根据服务ID获取端口号
	GetServicePorts(serviceID string) []string
	// GetInstanceLabels Get the label of all instances under a service
	GetInstanceLabels(serviceID string) *apiservice.InstanceLabels
}

// instanceCache 实例缓存的类
type instanceCache struct {
	*baseCache

	storage            store.Store
	lastMtimeLogged    int64
	ids                *sync.Map // instanceid -> instance
	services           *sync.Map // service id -> [instanceid ->instance]
	instanceCounts     *sync.Map // service id -> [instanceCount]
	servicePortsBucket *servicePortsBucket
	revisionCh         chan *revisionNotify
	disableBusiness    bool
	needMeta           bool
	systemServiceID    []string
	singleFlight       *singleflight.Group
	instanceCount      int64
	lastCheckAllTime   int64
}

func init() {
	RegisterCache(InstanceName, CacheInstance)
}

// newInstanceCache 新建一个instanceCache
func newInstanceCache(storage store.Store, ch chan *revisionNotify) *instanceCache {
	return &instanceCache{
		baseCache:  newBaseCache(storage),
		storage:    storage,
		revisionCh: ch,
	}
}

// initialize 初始化函数
func (ic *instanceCache) initialize(opt map[string]interface{}) error {
	ic.singleFlight = new(singleflight.Group)
	ic.ids = new(sync.Map)
	ic.services = new(sync.Map)
	ic.instanceCounts = new(sync.Map)
	ic.servicePortsBucket = &servicePortsBucket{
		lock:         sync.RWMutex{},
		servicePorts: make(map[string]map[string]struct{}),
	}
	if opt == nil {
		return nil
	}
	ic.disableBusiness, _ = opt["disableBusiness"].(bool)
	ic.needMeta, _ = opt["needMeta"].(bool)
	// 只加载系统服务
	if ic.disableBusiness {
		services, err := ic.getSystemServices()
		if err != nil {
			return err
		}
		ic.systemServiceID = make([]string, 0, len(services))
		for _, service := range services {
			if service.IsAlias() {
				continue
			}
			ic.systemServiceID = append(ic.systemServiceID, service.ID)
		}
	}
	return nil
}

// update 更新缓存函数
func (ic *instanceCache) update() error {
	// 多个线程竞争，只有一个线程进行更新
	_, err, _ := ic.singleFlight.Do(ic.name(), func() (interface{}, error) {
		defer func() {
			ic.lastMtimeLogged = logLastMtime(ic.lastMtimeLogged, ic.LastMtime().Unix(), "Instance")
			ic.checkAll()
			ic.reportMetricsInfo()
		}()
		return nil, ic.doCacheUpdate(ic.name(), ic.realUpdate)
	})
	return err
}

func (ic *instanceCache) LastMtime() time.Time {
	return ic.baseCache.LastMtime(ic.name())
}

func (ic *instanceCache) checkAll() {
	curTimeSec := time.Now().Unix()
	if curTimeSec-ic.lastCheckAllTime < checkAllIntervalSec {
		return
	}
	defer func() {
		ic.lastCheckAllTime = curTimeSec
	}()
	count, err := ic.storage.GetInstancesCount()
	if err != nil {
		log.Errorf("[Cache][Instance] get instance count from storage err: %s", err.Error())
		return
	}
	if ic.instanceCount == int64(count) {
		return
	}
	log.Infof(
		"[Cache][Instance] instance count not match, expect %d, actual %d, fallback to load all",
		count, ic.instanceCount)
	ic.resetLastMtime(ic.name())
	ic.resetLastFetchTime()
}

const maxLoadTimeDuration = 1 * time.Second

func (ic *instanceCache) realUpdate() (map[string]time.Time, int64, error) {
	// 拉取diff前的所有数据
	start := time.Now()
	instances, err := ic.storage.GetMoreInstances(ic.LastFetchTime(), ic.isFirstUpdate(), ic.needMeta, ic.systemServiceID)
	if err != nil {
		log.Errorf("[Cache][Instance] update get storage more err: %s", err.Error())
		return nil, -1, err
	}

	lastMimes, update, del := ic.setInstances(instances)
	timeDiff := time.Since(start)
	if timeDiff > 1*time.Second {
		log.Info("[Cache][Instance] get more instances",
			zap.Int("update", update), zap.Int("delete", del),
			zap.Time("last", ic.LastMtime()), zap.Duration("used", time.Since(start)))
	}
	return lastMimes, int64(len(instances)), err
}

// clear 清理内部缓存数据
func (ic *instanceCache) clear() error {
	ic.baseCache.clear()
	ic.ids = new(sync.Map)
	ic.services = new(sync.Map)
	ic.instanceCounts = new(sync.Map)
	ic.servicePortsBucket.reset()
	ic.instanceCount = 0
	return nil
}

// name 获取资源名称
func (ic *instanceCache) name() string {
	return InstanceName
}

// getSystemServices 获取系统服务ID
func (ic *instanceCache) getSystemServices() ([]*model.Service, error) {
	services, err := ic.storage.GetSystemServices()
	if err != nil {
		log.Errorf("[Cache][Instance] get system services err: %s", err.Error())
		return nil, err
	}
	return services, nil
}

// setInstances 保存instance到内存中
// 返回：更新个数，删除个数
func (ic *instanceCache) setInstances(ins map[string]*model.Instance) (map[string]time.Time, int, int) {
	if len(ins) == 0 {
		return nil, 0, 0
	}

	addInstances := map[string]string{}
	updateInstances := map[string]string{}
	deleteInstances := map[string]string{}

	lastMtime := ic.LastMtime().Unix()
	update := 0
	del := 0
	affect := make(map[string]bool)
	progress := 0
	instanceCount := ic.instanceCount

	for _, item := range ins {
		progress++
		if progress%50000 == 0 {
			log.Infof("[Cache][Instance] set instances progress: %d / %d", progress, len(ins))
		}
		modifyTime := item.ModifyTime.Unix()
		if lastMtime < modifyTime {
			lastMtime = modifyTime
		}
		affect[item.ServiceID] = true
		_, itemExist := ic.ids.Load(item.ID())
		// 待删除的instance
		if !item.Valid {
			deleteInstances[item.ID()] = item.Revision()
			del++
			ic.ids.Delete(item.ID())
			if itemExist {
				ic.manager.onEvent(item, EventDeleted)
				instanceCount--
			}
			value, ok := ic.services.Load(item.ServiceID)
			if !ok {
				continue
			}

			value.(*sync.Map).Delete(item.ID())
			continue
		}
		// 有修改或者新增的数据
		update++
		// 缓存的instance map增加一个version和protocol字段
		if item.Proto.Metadata == nil {
			item.Proto.Metadata = make(map[string]string)
		}

		item = fillInternalLabels(item)

		ic.ids.Store(item.ID(), item)
		if !itemExist {
			addInstances[item.ID()] = item.Revision()
			instanceCount++
			ic.manager.onEvent(item, EventCreated)
		} else {
			updateInstances[item.ID()] = item.Revision()
			ic.manager.onEvent(item, EventUpdated)
		}
		value, ok := ic.services.Load(item.ServiceID)
		if !ok {
			value = new(sync.Map)
			ic.services.Store(item.ServiceID, value)
		}

		ic.servicePortsBucket.appendPort(item.ServiceID, int(item.Port()))

		value.(*sync.Map).Store(item.ID(), item)
	}

	if ic.instanceCount != instanceCount {
		log.Infof("[Cache][Instance] instance count update from %d to %d",
			ic.instanceCount, instanceCount)
		ic.instanceCount = instanceCount
	}

	log.Info("[Cache][Instance] instances change info", zap.Any("add", addInstances),
		zap.Any("update", updateInstances), zap.Any("delete", deleteInstances))

	ic.postProcessUpdatedServices(affect)
	ic.manager.onEvent(affect, EventInstanceReload)
	return map[string]time.Time{
		ic.name(): time.Unix(lastMtime, 0),
	}, update, del
}

func fillInternalLabels(item *model.Instance) *model.Instance {
	if len(item.Version()) > 0 {
		item.Proto.Metadata["version"] = item.Version()
	}
	if len(item.Protocol()) > 0 {
		item.Proto.Metadata["protocol"] = item.Protocol()
	}

	if item.Location() != nil {
		item.Proto.Metadata["region"] = item.Location().GetRegion().GetValue()
		item.Proto.Metadata["zone"] = item.Location().GetZone().GetValue()
		item.Proto.Metadata["campus"] = item.Location().GetCampus().GetValue()
	}
	return item
}

func (ic *instanceCache) postProcessUpdatedServices(affect map[string]bool) {
	progress := 0
	for serviceID := range affect {
		ic.revisionCh <- newRevisionNotify(serviceID, true)
		progress++
		if progress%10000 == 0 {
			log.Infof("[Cache][Instance] revision notify progress(%d / %d)", progress, len(affect))
		}
		// 构建服务数量统计
		value, ok := ic.services.Load(serviceID)
		if !ok {
			ic.instanceCounts.Delete(serviceID)
			continue
		}
		count := &model.InstanceCount{}
		value.(*sync.Map).Range(func(key, item interface{}) bool {
			count.TotalInstanceCount++
			instance := item.(*model.Instance)
			if isInstanceHealthy(instance) {
				count.HealthyInstanceCount++
			}
			if instance.Proto.GetIsolate().GetValue() {
				count.IsolateInstanceCount++
			}
			return true
		})
		if count.TotalInstanceCount == 0 {
			ic.instanceCounts.Delete(serviceID)
			continue
		}
		ic.instanceCounts.Store(serviceID, count)
	}
}

func isInstanceHealthy(instance *model.Instance) bool {
	return instance.Proto.GetHealthy().GetValue() && !instance.Proto.GetIsolate().GetValue()
}

// GetInstance 根据实例ID获取实例数据
func (ic *instanceCache) GetInstance(instanceID string) *model.Instance {
	if instanceID == "" {
		return nil
	}

	value, ok := ic.ids.Load(instanceID)
	if !ok {
		return nil
	}

	return value.(*model.Instance)
}

// GetInstancesByServiceID 根据ServiceID获取实例数据
func (ic *instanceCache) GetInstancesByServiceID(serviceID string) []*model.Instance {
	if serviceID == "" {
		return nil
	}

	value, ok := ic.services.Load(serviceID)
	if !ok {
		return nil
	}

	var out []*model.Instance
	value.(*sync.Map).Range(func(k interface{}, v interface{}) bool {
		out = append(out, v.(*model.Instance))
		return true
	})

	return out
}

// GetInstancesCountByServiceID 根据服务ID获取实例数
func (ic *instanceCache) GetInstancesCountByServiceID(serviceID string) model.InstanceCount {
	if serviceID == "" {
		return model.InstanceCount{}
	}

	value, ok := ic.instanceCounts.Load(serviceID)
	if !ok {
		return model.InstanceCount{}
	}
	return *(value.(*model.InstanceCount))
}

// IteratorInstances 迭代所有的instance的函数
func (ic *instanceCache) IteratorInstances(iterProc InstanceIterProc) error {
	return iteratorInstancesProc(ic.ids, iterProc)
}

// IteratorInstancesWithService 根据服务ID进行迭代回调
func (ic *instanceCache) IteratorInstancesWithService(serviceID string, iterProc InstanceIterProc) error {
	if serviceID == "" {
		return nil
	}
	value, ok := ic.services.Load(serviceID)
	if !ok {
		return nil
	}

	return iteratorInstancesProc(value.(*sync.Map), iterProc)
}

// GetInstancesCount 获取实例的个数
func (ic *instanceCache) GetInstancesCount() int {
	count := 0
	ic.ids.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	return count
}

// GetInstanceLabels 获取某个服务下实例的所有标签信息集合
func (ic *instanceCache) GetInstanceLabels(serviceID string) *apiservice.InstanceLabels {
	if serviceID == "" {
		return &apiservice.InstanceLabels{}
	}

	value, ok := ic.services.Load(serviceID)
	if !ok {
		return &apiservice.InstanceLabels{}
	}

	ret := &apiservice.InstanceLabels{
		Labels: make(map[string]*apimodel.StringList),
	}

	tmp := make(map[string]map[string]struct{})
	_ = iteratorInstancesProc(value.(*sync.Map), func(key string, value *model.Instance) (bool, error) {
		metadata := value.Metadata()
		for k, v := range metadata {
			if _, ok := tmp[k]; !ok {
				tmp[k] = make(map[string]struct{})
			}
			tmp[k][v] = struct{}{}
		}
		return true, nil
	})

	for k, v := range tmp {
		if _, ok := ret.Labels[k]; !ok {
			ret.Labels[k] = &apimodel.StringList{Values: make([]string, 0, 4)}
		}

		for vv := range v {
			ret.Labels[k].Values = append(ret.Labels[k].Values, vv)
		}
	}

	return ret
}

func (ic *instanceCache) GetServicePorts(serviceID string) []string {
	return ic.servicePortsBucket.listPort(serviceID)
}

// iteratorInstancesProc 迭代指定的instance数据，id->instance
func iteratorInstancesProc(data *sync.Map, iterProc InstanceIterProc) error {
	var (
		cont bool
		err  error
	)

	proc := func(k, v interface{}) bool {
		cont, err = iterProc(k.(string), v.(*model.Instance))
		if err != nil {
			return false
		}
		return cont
	}

	data.Range(proc)
	return err
}
