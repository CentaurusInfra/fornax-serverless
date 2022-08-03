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
	node_config "centaurusinfra.io/fornax-serverless/pkg/nodeagent/config"
	"github.com/spf13/pflag"
)

type SimulationNodeConfiguration struct {
	node_config.NodeConfiguration
	NumOfNode int
}

func AddConfigFlags(flagSet *pflag.FlagSet, nodeConfig *SimulationNodeConfiguration) {
	node_config.AddConfigFlags(flagSet, &nodeConfig.NodeConfiguration)
	flagSet.IntVar(&nodeConfig.NumOfNode, "num-of-node", nodeConfig.NumOfNode, "how many nodes are simulated")
}

func DefaultNodeConfiguration() (*SimulationNodeConfiguration, error) {
	c, err := node_config.DefaultNodeConfiguration()
	if err != nil {
		return nil, err
	}
	return &SimulationNodeConfiguration{
		NodeConfiguration: *c,
		NumOfNode:         0,
	}, nil
}

func ValidateNodeConfiguration(*SimulationNodeConfiguration) []error {
	return nil
}
