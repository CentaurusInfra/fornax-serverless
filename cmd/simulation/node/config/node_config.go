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
package config

import (
	"fmt"

	nconfig "centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/network"
	"github.com/spf13/pflag"
)

type SimulationNodeConfiguration struct {
	NodeConfig     nconfig.NodeConfiguration
	NodeIP         string
	FornaxCoreUrls []string
	NumOfNode      int
	PodConcurrency int
}

func AddConfigFlags(flagSet *pflag.FlagSet, nodeConfig *SimulationNodeConfiguration) {
	flagSet.StringVar(&nodeConfig.NodeIP, "node-ip", nodeConfig.NodeIP, "IPv4 addresses of the node. If unset, use the node's default IPv4 address")

	flagSet.StringArrayVar(&nodeConfig.FornaxCoreUrls, "fornaxcore-ip", nodeConfig.FornaxCoreUrls, "IPv4 addresses of the fornaxcores. must provided")

	flagSet.IntVar(&nodeConfig.NumOfNode, "num-of-node", nodeConfig.NumOfNode, "how many nodes are simulated")

	flagSet.IntVar(&nodeConfig.PodConcurrency, "concurrency-of-pod-operation", nodeConfig.PodConcurrency, "how many pods are allowed to create or terminated in parallel")
}

func DefaultNodeConfiguration() (*SimulationNodeConfiguration, error) {
	ips, err := network.GetLocalV4IP()
	if err != nil {
		return nil, err
	}
	nodeIp := ips[0].To4().String()
	nodeConfig, _ := nconfig.DefaultNodeConfiguration()

	return &SimulationNodeConfiguration{
		NodeConfig:     *nodeConfig,
		NodeIP:         nodeIp,
		FornaxCoreUrls: []string{fmt.Sprintf("%s:18001", nodeIp)},
		NumOfNode:      1,
		PodConcurrency: 5,
	}, nil
}

func ValidateNodeConfiguration(*SimulationNodeConfiguration) []error {
	return nil
}
