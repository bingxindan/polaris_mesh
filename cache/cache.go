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
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"sync"
	"time"

	"github.com/polarismesh/polaris/common/metrics"
	"github.com/polarismesh/polaris/common/model"
	"github.com/polarismesh/polaris/store"
)

var (
	cacheSet = map[string]int{}

	_ InstanceCache       = (*instanceCache)(nil)
	_ ServiceCache        = (*serviceCache)(nil)
	_ RoutingConfigCache  = (*routingConfigCache)(nil)
	_ CircuitBreakerCache = (*circuitBreakerCache)(nil)
	_ RateLimitCache      = (*rateLimitCache)(nil)
	_ NamespaceCache      = (*namespaceCache)(nil)
	_ ClientCache         = (*clientCache)(nil)
	_ UserCache           = (*userCache)(nil)
	_ StrategyCache       = (*strategyCache)(nil)
	_ L5Cache             = (*l5Cache)(nil)
	_ FileCache           = (*fileCache)(nil)
	_ FaultDetectCache    = (*faultDetectCache)(nil)
)

const (
	// CacheNamespace int = iota
	// CacheBusiness
	CacheService = iota
	CacheInstance
	CacheRoutingConfig
	CacheCL5
	CacheRateLimit
	CacheCircuitBreaker
	CacheUser
	CacheAuthStrategy
	CacheNamespace
	CacheClient
	CacheConfigFile
	CacheFaultDetector

	CacheLast
)

// CacheName cache name
type CacheName string

const (
	CacheNameService         CacheName = "Service"
	CacheNameInstance        CacheName = "Instance"
	CacheNameRoutingConfig   CacheName = "RoutingConfig"
	CacheNameCL5             CacheName = "CL5"
	CacheNameRateLimit       CacheName = "RateLimit"
	CacheNameCircuitBreaker  CacheName = "CircuitBreaker"
	CacheNameUser            CacheName = "User"
	CacheNameAuthStrategy    CacheName = "AuthStrategy"
	CacheNameNamespace       CacheName = "Namespace"
	CacheNameClient          CacheName = "Client"
	CacheNameConfigFile      CacheName = "ConfigFile"
	CacheNameFaultDetectRule CacheName = "FaultDetectRule"
)

var (
	cacheIndexMap = map[CacheName]int{
		CacheNameService:         CacheService,
		CacheNameInstance:        CacheInstance,
		CacheNameRoutingConfig:   CacheRoutingConfig,
		CacheNameCL5:             CacheCL5,
		CacheNameRateLimit:       CacheRateLimit,
		CacheNameCircuitBreaker:  CacheCircuitBreaker,
		CacheNameUser:            CacheUser,
		CacheNameAuthStrategy:    CacheAuthStrategy,
		CacheNameNamespace:       CacheNamespace,
		CacheNameClient:          CacheClient,
		CacheNameConfigFile:      CacheConfigFile,
		CacheNameFaultDetectRule: CacheFaultDetector,
	}
)

var (
	// DefaultTimeDiff default time diff
	DefaultTimeDiff = -5 * time.Second
)

// Cache 缓存接口
type Cache interface {
	// initialize
	initialize(c map[string]interface{}) error

	// addListener 添加
	addListener(listeners []Listener)

	// update
	update() error

	// clear
	clear() error

	// name
	name() string
}

// baseCache 对于 Cache 中的一些 func 做统一实现，避免重复逻辑
type baseCache struct {
	lock sync.RWMutex
	// firtstUpdate Whether the cache is loaded for the first time
	// this field can only make value on exec initialize/clean, and set it to false on exec update
	firtstUpdate  bool
	s             store.Store
	lastFetchTime int64
	lastMtimes    map[string]time.Time
	manager       *listenerManager
}

func newBaseCache(s store.Store) *baseCache {
	c := &baseCache{
		s: s,
	}

	c.initialize()
	return c
}

func (bc *baseCache) initialize() {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	bc.lastFetchTime = 1
	bc.firtstUpdate = true
	bc.manager = &listenerManager{
		listeners: make([]Listener, 0, 4),
	}
	bc.lastMtimes = map[string]time.Time{}
}

var (
	zeroTime = time.Unix(0, 0)
)

func (bc *baseCache) resetLastMtime(label string) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	bc.lastMtimes[label] = time.Unix(0, 0)
}

func (bc *baseCache) resetLastFetchTime() {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	bc.lastFetchTime = 1
}

func (bc *baseCache) LastMtime(label string) time.Time {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	v, ok := bc.lastMtimes[label]
	if ok {
		return v
	}

	return time.Unix(0, 0)
}

func (bc *baseCache) LastFetchTime() time.Time {
	lastTime := time.Unix(bc.lastFetchTime, 0)
	tmp := lastTime.Add(DefaultTimeDiff)
	if zeroTime.After(tmp) {
		return lastTime
	}
	lastTime = tmp
	return lastTime
}

func (bc *baseCache) isFirstUpdate() bool {
	return bc.firtstUpdate
}

// update
func (bc *baseCache) doCacheUpdate(name string, executor func() (map[string]time.Time, int64, error)) error {
	curStoreTime, err := bc.s.GetUnixSecond(0)
	if err != nil {
		curStoreTime = bc.lastFetchTime
		log.Warnf("[Cache][%s] get store timestamp fail, skip update lastMtime, err : %v", name, err)
	}
	defer func() {
		bc.lastFetchTime = curStoreTime
	}()

	start := time.Now()
	lastMtimes, total, err := executor()
	if err != nil {
		return err
	}

	bc.lock.Lock()
	defer bc.lock.Unlock()
	if len(lastMtimes) != 0 {
		if len(bc.lastMtimes) != 0 {
			for label, lastMtime := range lastMtimes {
				preLastMtime := bc.lastMtimes[label]
				log.Infof("[Cache][%s] lastFetchTime %s, lastMtime update from %s to %s",
					label, time.Unix(bc.lastFetchTime, 0), preLastMtime, lastMtime)
			}
		}
		bc.lastMtimes = lastMtimes
	}

	if total >= 0 {
		metrics.RecordCacheUpdateCost(time.Since(start), name, total)
	}
	bc.firtstUpdate = false
	return nil
}

func (bc *baseCache) clear() {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	bc.lastMtimes = make(map[string]time.Time)
	bc.lastFetchTime = 1
	bc.firtstUpdate = true
}

// addListener 添加
func (bc *baseCache) addListener(listeners []Listener) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	bc.manager.listeners = append(bc.manager.listeners, listeners...)
}

const (
	// UpdateCacheInterval 缓存更新时间间隔
	UpdateCacheInterval = 1 * time.Second
)

const (
	// RevisionConcurrenceCount Revision计算的并发线程数
	RevisionConcurrenceCount = 64
	// RevisionChanCount 存储revision计算的通知管道，可以稍微设置大一点
	RevisionChanCount = 102400
)

// 更新revision的结构体
type revisionNotify struct {
	serviceID string
	valid     bool
}

// create new revision notify
func newRevisionNotify(serviceID string, valid bool) *revisionNotify {
	return &revisionNotify{
		serviceID: serviceID,
		valid:     valid,
	}
}

// CacheManager 名字服务缓存
type CacheManager struct {
	storage store.Store
	caches  []Cache

	comRevisionCh chan *revisionNotify
	revisions     map[string]string // service id -> reversion (所有instance reversion 的累计计算值)
	lock          sync.RWMutex      // for revisions rw lock
}

// initialize 缓存对象初始化
func (nc *CacheManager) initialize() error {
	if config.DiffTime != 0 {
		DefaultTimeDiff = config.DiffTime
	}

	for _, obj := range nc.caches {
		var option map[string]interface{}
		for _, entry := range config.Resources {
			if obj.name() == entry.Name {
				option = entry.Option
				break
			}
		}
		if err := obj.initialize(option); err != nil {
			return err
		}
	}

	return nil
}

// update 缓存更新
func (nc *CacheManager) update() error {
	var wg sync.WaitGroup
	for _, entry := range config.Resources {
		index, exist := cacheSet[entry.Name]
		if !exist {
			return fmt.Errorf("cache resource %s not exists", entry.Name)
		}
		wg.Add(1)
		go func(c Cache) {
			defer wg.Done()
			_ = c.update()
		}(nc.caches[index])
	}

	wg.Wait()
	return nil
}

func (nc *CacheManager) deleteRevisions(id string) {
	nc.lock.Lock()
	delete(nc.revisions, id)
	nc.lock.Unlock()
}

func (nc *CacheManager) setRevisions(key string, val string) {
	nc.lock.Lock()
	nc.revisions[key] = val
	nc.lock.Unlock()
}

func (nc *CacheManager) readRevisions(key string) (string, bool) {
	nc.lock.RLock()
	defer nc.lock.RUnlock()

	id, ok := nc.revisions[key]
	return id, ok
}

// clear 清除caches的所有缓存数据
func (nc *CacheManager) clear() error {
	for _, obj := range nc.caches {
		if err := obj.clear(); err != nil {
			return err
		}
	}

	return nil
}

// Start 缓存对象启动协程，定时更新缓存
func (nc *CacheManager) Start(ctx context.Context) error {
	log.Infof("[Cache] cache goroutine start")
	// 先启动revision计算协程
	go nc.revisionWorker(ctx)

	// 启动的时候，先更新一版缓存
	log.Infof("[Cache] cache update now first time")
	if err := nc.update(); err != nil {
		return err
	}
	log.Infof("[Cache] cache update done")

	// 启动协程，开始定时更新缓存数据
	go func() {
		ticker := time.NewTicker(nc.GetUpdateCacheInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_ = nc.update()
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// Clear 主动清除缓存数据
func (nc *CacheManager) Clear() error {
	nc.lock.Lock()
	nc.revisions = map[string]string{}
	nc.lock.Unlock()

	return nc.clear()
}

// revisionWorker Cache中计算服务实例revision的worker
func (nc *CacheManager) revisionWorker(ctx context.Context) {
	log.Infof("[Cache] compute revision worker start")
	defer log.Infof("[Cache] compute revision worker done")

	// 启动多个协程来计算revision，后续可以通过启动参数控制
	for i := 0; i < RevisionConcurrenceCount; i++ {
		go func() {
			for {
				select {
				case req := <-nc.comRevisionCh:
					if ok := nc.processRevisionWorker(req); !ok {
						continue
					}

					// 每个计算完，等待2ms
					time.Sleep(2 * time.Millisecond)
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

// processRevisionWorker 处理revision计算的函数
func (nc *CacheManager) processRevisionWorker(req *revisionNotify) bool {
	if req == nil {
		log.Errorf("[Cache][Revision] get null revision request")
		return false
	}

	if req.serviceID == "" {
		log.Errorf("[Cache][Revision] get request service ID is empty")
		return false
	}

	if !req.valid {
		log.Infof("[Cache][Revision] service(%s) revision has all been removed", req.serviceID)
		nc.deleteRevisions(req.serviceID)
		return true
	}

	service := nc.Service().GetServiceByID(req.serviceID)
	if service == nil {
		// log.Errorf("[Cache][Revision] can not found service id(%s)", req.serviceID)
		return false
	}

	instances := nc.Instance().GetInstancesByServiceID(req.serviceID)
	revision, err := ComputeRevision(service.Revision, instances)
	if err != nil {
		log.Errorf(
			"[Cache] compute service id(%s) instances revision err: %s", req.serviceID, err.Error())
		return false
	}

	nc.setRevisions(req.serviceID, revision) // string -> string
	log.Infof("[Cache] compute service id(%s) instances revision : %s", req.serviceID, revision)
	return true
}

// GetUpdateCacheInterval 获取当前cache的更新间隔
func (nc *CacheManager) GetUpdateCacheInterval() time.Duration {
	return UpdateCacheInterval
}

// GetServiceInstanceRevision 获取服务实例计算之后的revision
func (nc *CacheManager) GetServiceInstanceRevision(serviceID string) string {
	value, ok := nc.readRevisions(serviceID)
	if !ok {
		return ""
	}

	return value
}

// GetServiceRevisionCount 计算一下缓存中的revision的个数
func (nc *CacheManager) GetServiceRevisionCount() int {
	nc.lock.RLock()
	defer nc.lock.RUnlock()

	return len(nc.revisions)
}

func (nc *CacheManager) AddListener(cacheName CacheName, listeners []Listener) {
	cacheIndex := cacheIndexMap[cacheName]
	nc.caches[cacheIndex].addListener(listeners)
}

// Service 获取Service缓存信息
func (nc *CacheManager) Service() ServiceCache {
	return nc.caches[CacheService].(ServiceCache)
}

// Instance 获取Instance缓存信息
func (nc *CacheManager) Instance() InstanceCache {
	return nc.caches[CacheInstance].(InstanceCache)
}

// RoutingConfig 获取路由配置的缓存信息
func (nc *CacheManager) RoutingConfig() RoutingConfigCache {
	return nc.caches[CacheRoutingConfig].(RoutingConfigCache)
}

// CL5 获取l5缓存信息
func (nc *CacheManager) CL5() L5Cache {
	return nc.caches[CacheCL5].(L5Cache)
}

// RateLimit 获取限流规则缓存信息
func (nc *CacheManager) RateLimit() RateLimitCache {
	return nc.caches[CacheRateLimit].(RateLimitCache)
}

// CircuitBreaker 获取熔断规则缓存信息
func (nc *CacheManager) CircuitBreaker() CircuitBreakerCache {
	return nc.caches[CacheCircuitBreaker].(CircuitBreakerCache)
}

// FaultDetector 获取探测规则缓存信息
func (nc *CacheManager) FaultDetector() FaultDetectCache {
	return nc.caches[CacheFaultDetector].(FaultDetectCache)
}

// User Get user information cache information
func (nc *CacheManager) User() UserCache {
	return nc.caches[CacheUser].(UserCache)
}

// AuthStrategy Get authentication cache information
func (nc *CacheManager) AuthStrategy() StrategyCache {
	return nc.caches[CacheAuthStrategy].(StrategyCache)
}

// Namespace Get namespace cache information
func (nc *CacheManager) Namespace() NamespaceCache {
	return nc.caches[CacheNamespace].(NamespaceCache)
}

// Client Get client cache information
func (nc *CacheManager) Client() ClientCache {
	return nc.caches[CacheClient].(ClientCache)
}

// ConfigFile get config file cache information
func (nc *CacheManager) ConfigFile() FileCache {
	return nc.caches[CacheConfigFile].(FileCache)
}

// GetStore get store
func (nc *CacheManager) GetStore() store.Store {
	return nc.storage
}

// ComputeRevision 计算唯一的版本标识
func ComputeRevision(serviceRevision string, instances []*model.Instance) (string, error) {
	h := sha1.New()
	if _, err := h.Write([]byte(serviceRevision)); err != nil {
		return "", err
	}

	var slice sort.StringSlice
	for _, item := range instances {
		slice = append(slice, item.Revision())
	}
	if len(slice) > 0 {
		slice.Sort()
	}
	return ComputeRevisionBySlice(h, slice)
}

func ComputeRevisionBySlice(h hash.Hash, slice []string) (string, error) {
	for _, revision := range slice {
		if _, err := h.Write([]byte(revision)); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CompositeComputeRevision 将多个 revision 合并计算为一个
func CompositeComputeRevision(revisions []string) (string, error) {
	h := sha1.New()

	sort.Strings(revisions)

	for i := range revisions {
		if _, err := h.Write([]byte(revisions[i])); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// RegisterCache 注册缓存资源
func RegisterCache(name string, index int) {
	if _, exist := cacheSet[name]; exist {
		panic(fmt.Sprintf("existed cache resource: name = %s", name))
	}

	cacheSet[name] = index
}

const mtimeLogIntervalSec = 120

// logLastMtime 定时打印mtime更新结果
func logLastMtime(lastMtimeLogged int64, lastMtime int64, prefix string) int64 {
	curTimeSec := time.Now().Unix()
	if lastMtimeLogged == 0 || curTimeSec-lastMtimeLogged >= mtimeLogIntervalSec {
		lastMtimeLogged = curTimeSec
		log.Infof("[Cache][%s] current lastMtime is %s", prefix, time.Unix(lastMtime, 0))
	}
	return lastMtimeLogged
}
