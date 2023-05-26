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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/polarismesh/polaris/common/metrics"
	"github.com/polarismesh/polaris/plugin"
)

func (fc *fileCache) reportMetricsInfo(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	var preGroup map[string]map[string]struct{}

	for {
		select {
		case <-ticker.C:
			tmpGroup := map[string]map[string]struct{}{}

			configGroups, err := fc.storage.CountGroupEachNamespace()
			if err != nil {
				log.Error("[Cache][ConfigFile] report metrics for config_group each namespace", zap.Error(err))
				continue
			}

			configFiles, err := fc.storage.CountConfigFileEachGroup()
			if err != nil {
				log.Error("[Cache][ConfigFile] report metrics for config_file each group", zap.Error(err))
				continue
			}

			releaseFiles, err := fc.storage.CountConfigFileReleaseEachGroup()
			if err != nil {
				log.Error("[Cache][ConfigFile] report metrics for release config_file each group", zap.Error(err))
				continue
			}
			for ns, groups := range configFiles {
				if _, ok := tmpGroup[ns]; !ok {
					tmpGroup[ns] = map[string]struct{}{}
				}
				for group := range groups {
					tmpGroup[ns][group] = struct{}{}
				}
			}

			metricValues := make([]metrics.ConfigMetrics, 0, 64)

			for ns := range configGroups {
				metricValues = append(metricValues, metrics.ConfigMetrics{
					Type:    metrics.ConfigGroupMetric,
					Total:   configGroups[ns],
					Release: 0,
					Labels: map[string]string{
						metrics.LabelNamespace: ns,
					},
				})
			}

			for ns, groups := range configFiles {
				for group, total := range groups {
					metricValues = append(metricValues, metrics.ConfigMetrics{
						Type:  metrics.FileMetric,
						Total: total,
						Labels: map[string]string{
							metrics.LabelNamespace: ns,
							metrics.LabelGroup:     group,
						},
					})
				}
			}

			for ns, groups := range releaseFiles {
				for group, total := range groups {
					metricValues = append(metricValues, metrics.ConfigMetrics{
						Type:  metrics.ReleaseFileMetric,
						Total: total,
						Labels: map[string]string{
							metrics.LabelNamespace: ns,
							metrics.LabelGroup:     group,
						},
					})
				}
			}
			cleanExpireConfigFileMetricLabel(preGroup, tmpGroup)
			preGroup = tmpGroup
			plugin.GetStatis().ReportConfigMetrics(metricValues...)
		case <-ctx.Done():
			return
		}
	}
}

func cleanExpireConfigFileMetricLabel(pre, curr map[string]map[string]struct{}) {
	if len(pre) == 0 {
		return
	}

	var (
		removeNs = map[string]struct{}{}
		remove   = map[string]map[string]struct{}{}
	)

	for ns, groups := range pre {
		if _, ok := curr[ns]; !ok {
			removeNs[ns] = struct{}{}
		}
		if _, ok := remove[ns]; !ok {
			remove[ns] = map[string]struct{}{}
		}
		for group := range groups {
			if _, ok := curr[ns][group]; !ok {
				remove[ns][group] = struct{}{}
			}
		}
	}

	for ns := range removeNs {
		metrics.GetConfigGroupTotal().Delete(prometheus.Labels{
			metrics.LabelNamespace: ns,
		})
	}

	for ns, groups := range remove {
		for group := range groups {
			metrics.GetConfigFileTotal().Delete(prometheus.Labels{
				metrics.LabelNamespace: ns,
				metrics.LabelGroup:     group,
			})
			metrics.GetReleaseConfigFileTotal().Delete(prometheus.Labels{
				metrics.LabelNamespace: ns,
				metrics.LabelGroup:     group,
			})
			metrics.GetConfigFileTotal().Delete(prometheus.Labels{
				metrics.LabelNamespace: ns,
				metrics.LabelGroup:     group,
			})
		}
	}

}
