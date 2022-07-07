/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cadvisor

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	_ "github.com/google/cadvisor/container/containerd/install"
	_ "github.com/google/cadvisor/container/systemd/install"

	"github.com/google/cadvisor/cache/memory"
	cadvisormetrics "github.com/google/cadvisor/container"
	cadvisorinfov1 "github.com/google/cadvisor/info/v1"
	cadvisorinfov2 "github.com/google/cadvisor/info/v2"
	"github.com/google/cadvisor/manager"
	"github.com/google/cadvisor/utils/sysfs"
	"k8s.io/klog/v2"
)

var _ CAdvisorInfoProvider = &cadvisorInfoProvider{}

type cadvisorInfoProvider struct {
	nodeCAdvisorInfo *NodeCAdvisorInfo
	nodeInfoInterval time.Duration
	done             bool
	receivers        map[string]*chan NodeCAdvisorInfo
	containers       map[string]bool
	runtime          runtime.RuntimeService
	cgroupRoots      []string
	rootPath         string
	realCAdvisor     manager.Manager
}

// GetNodeCAdvisorInfo implements CAdvisorInfoProvider
// if there is already cached node cadvisor info, return it, otherwise, create a new one
func (c *cadvisorInfoProvider) GetNodeCAdvisorInfo() (*NodeCAdvisorInfo, error) {
	if c.nodeCAdvisorInfo == nil {
		info, err := c.collectCAdvisorInfo()
		if err == nil {
			c.nodeCAdvisorInfo = info
		} else {
			klog.ErrorS(err, "failed to collect NodeCAdvisorInfo")
			return nil, err
		}
	}
	return c.nodeCAdvisorInfo, nil
}

// Stop implements CAdvisorInfoProvider
func (cc *cadvisorInfoProvider) Stop() error {
	cc.done = true
	return nil
}

// ReceiveCAdvisorInfo implements Interface
func (c *cadvisorInfoProvider) ReceiveCAdvisorInfo(id string, receiver *chan NodeCAdvisorInfo) {
	c.receivers[id] = receiver
}

func NewCAdvisorInfoProvider(
	config CAdvisorConfig,
	runtime runtime.RuntimeService,
) (CAdvisorInfoProvider, error) {
	if _, err := os.Stat(config.rootPath); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(path.Clean(config.rootPath), 0750); err != nil {
				return nil, fmt.Errorf("error creating root directory %q: %v", config.rootPath, err)
			}
		} else {
			return nil, fmt.Errorf("failed to Stat %q: %v", config.rootPath, err)
		}
	}

	includedMetrics := cadvisormetrics.MetricSet{
		cadvisormetrics.AppMetrics:              struct{}{},
		cadvisormetrics.AcceleratorUsageMetrics: struct{}{},
		cadvisormetrics.CpuUsageMetrics:         struct{}{},
		cadvisormetrics.CpuLoadMetrics:          struct{}{},
		cadvisormetrics.DiskIOMetrics:           struct{}{},
		cadvisormetrics.DiskUsageMetrics:        struct{}{},
		cadvisormetrics.MemoryUsageMetrics:      struct{}{},
		cadvisormetrics.NetworkUsageMetrics:     struct{}{},
		cadvisormetrics.ProcessMetrics:          struct{}{},
		cadvisormetrics.OOMMetrics:              struct{}{},
	}

	allow := true
	housekeepingConfig := manager.HouskeepingConfig{
		Interval:     &config.housekeepingInterval,
		AllowDynamic: &allow,
	}

	m, err := manager.New(
		memory.New(config.statsCacheDuration, nil),
		sysfs.NewRealSysFs(),
		housekeepingConfig,
		includedMetrics,
		http.DefaultClient,
		config.cGroupRoots,
		nil,
		"",
		time.Duration(0))
	if err != nil {
		return nil, err
	}
	err = m.Start()
	if err != nil {
		return nil, err
	}

	return &cadvisorInfoProvider{
		nodeInfoInterval: config.nodeInfoInterval,
		done:             false,
		receivers:        map[string]*chan NodeCAdvisorInfo{},
		containers:       map[string]bool{},
		runtime:          runtime,
		cgroupRoots:      config.cGroupRoots,
		rootPath:         config.rootPath,
		realCAdvisor:     m,
	}, nil
}

func (cc *cadvisorInfoProvider) Start() error {
	if err := cc.realCAdvisor.Start(); err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(cc.nodeInfoInterval)
		for {
			if cc.done {
				ticker.Stop()
				break
			}

			event, err := cc.collectCAdvisorInfo()
			if err == nil {
				cc.nodeCAdvisorInfo = event

				panicReceivers := make(map[string]bool)
				for n, r := range cc.receivers {
					klog.Infof("send node cavisor info to receiver %s", n)
					func() {
						defer func() {
							if err := recover(); err != nil {
								klog.Errorf("send message panic occurred: %v", err)
								// remember it and remove closed channel after loop
								panicReceivers[n] = true
							}
						}()
						*r <- *event
					}()
				}

				for r := range panicReceivers {
					delete(cc.receivers, r)
				}
			}

			select {
			case _ = <-ticker.C:
				// ticking, send new event
			}
		}
	}()

	return nil
}

func (cc *cadvisorInfoProvider) collectCAdvisorInfo() (*NodeCAdvisorInfo, error) {
	event := NodeCAdvisorInfo{
		MachineInfo:   nil,
		RootFsInfo:    nil,
		ImageFsInfo:   nil,
		ContainerInfo: []*cadvisorinfov2.ContainerInfo{},
	}

	// get cached machine info if it's already loaded
	if cc.nodeCAdvisorInfo != nil && cc.nodeCAdvisorInfo.MachineInfo != nil {
		event.MachineInfo = cc.nodeCAdvisorInfo.MachineInfo
	} else {
		if machineInfo, err := cc.collectCAdvisorMachineInfo(); err == nil {
			event.MachineInfo = machineInfo
		} else {
			return nil, err
		}
	}

	// get cached version info if it's already loaded
	if cc.nodeCAdvisorInfo != nil && cc.nodeCAdvisorInfo.VersionInfo != nil {
		event.VersionInfo = cc.nodeCAdvisorInfo.VersionInfo
	} else {
		if versionInfo, err := cc.collectCAdvisorVersionInfo(); err == nil {
			event.VersionInfo = versionInfo
		} else {
			return nil, err
		}
	}

	if rootFsInfo, err := cc.collectCAdvisorDirFsInfo(cc.rootPath); err == nil {
		event.RootFsInfo = &rootFsInfo
	} else {
		return nil, err
	}

	// containerInfos := cc.collectCAdvisorContainerInfo()
	// for _, v := range containerInfos {
	// 	event.ContainerInfo = append(event.ContainerInfo, v)
	// }
	return &event, nil
}

func (cc *cadvisorInfoProvider) collectCAdvisorDirFsInfo(path string) (cadvisorinfov2.FsInfo, error) {
	return cc.realCAdvisor.GetDirFsInfo(path)
}

func (cc *cadvisorInfoProvider) collectCAdvisorMachineInfo() (*cadvisorinfov1.MachineInfo, error) {
	return cc.realCAdvisor.GetMachineInfo()
}

func (cc *cadvisorInfoProvider) collectCAdvisorVersionInfo() (*cadvisorinfov1.VersionInfo, error) {
	return cc.realCAdvisor.GetVersionInfo()
}

// func (cc *cadvisorInfoProvider) collectCAdvisorContainerInfo() map[string]*cadvisorinfov2.ContainerInfo {
// 	options := cadvisorinfov2.RequestOptions{
// 		IdType:    "name",
// 		Count:     1,
// 		Recursive: true,
// 		MaxAge:    nil,
// 	}
// 	containerInfos := make(map[string]*cadvisorinfov2.ContainerInfo)
//
// 	cc.getContainerList()
// 	for c := range cc.containers {
// 		if infos, err := cc.realCAdvisor.GetContainerInfoV2(c, options); err != nil {
// 			// skip get single container info error
// 			klog.Errorf("failed to get container cadvisor info: %v", err)
// 		} else {
// 			for n, info := range infos {
// 				containerInfos[n] = &info
// 			}
// 		}
// 	}
//
// 	return containerInfos
// }
