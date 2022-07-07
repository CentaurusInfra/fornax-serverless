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
	cadvisorv1 "github.com/google/cadvisor/info/v1"
	cadvisorv2 "github.com/google/cadvisor/info/v2"
)

type NodeCAdvisorInfo struct {
	ContainerInfo []*cadvisorv2.ContainerInfo
	MachineInfo   *cadvisorv1.MachineInfo
	RootFsInfo    *cadvisorv2.FsInfo
	ImageFsInfo   *cadvisorv2.FsInfo
	VersionInfo   *cadvisorv1.VersionInfo
}

type CAdvisorInfoProvider interface {
	Start() error
	Stop() error
	ReceiveCAdvisorInfo(id string, receiver *chan NodeCAdvisorInfo)
	GetNodeCAdvisorInfo() (*NodeCAdvisorInfo, error)
}
