package cadvisor

import (
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
)

const (
	DefaultNodeInfoInterval     = 10 * time.Second
	DefaultStatsCacheDuration   = 1 * time.Minute
	DefaultHousekeepingInterval = 10 * time.Minute
)

type CAdvisorConfig struct {
	rootPath             string
	nodeInfoInterval     time.Duration
	statsCacheDuration   time.Duration
	housekeepingInterval time.Duration
	cGroupRoots          []string
}

func DefaultCAdvisorConfig(nodeConfig config.NodeConfiguration) CAdvisorConfig {
	cgroupRoots := []string{nodeConfig.CgroupRoot}
	if len(nodeConfig.SystemCgroupName) > 0 {
		cgroupRoots = append(cgroupRoots, nodeConfig.SystemCgroupName)
	}

	if len(nodeConfig.NodeAgentCgroupName) > 0 {
		cgroupRoots = append(cgroupRoots, nodeConfig.NodeAgentCgroupName)
	}

	if len(nodeConfig.PodCgroupName) > 0 {
		cgroupRoots = append(cgroupRoots, nodeConfig.PodCgroupName)
	}

	return CAdvisorConfig{
		nodeInfoInterval:     DefaultNodeInfoInterval,
		statsCacheDuration:   DefaultStatsCacheDuration,
		housekeepingInterval: DefaultHousekeepingInterval,
		cGroupRoots:          cgroupRoots,
		rootPath:             nodeConfig.RootPath,
	}
}
