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

package container

import (
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/runtime"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	v1 "k8s.io/api/core/v1"
)

type PodContainerProbeResult struct {
	Result    ProbeResult
	ProbeType ProbeType
}

type ProbeType string

const (
	LivenessProbe                ProbeType = "liveness"
	ReadinessProbe               ProbeType = "readiness"
	StartupProbe                 ProbeType = "startup"
	RuntimeStatusProbe           ProbeType = "runtime"
	RunningContainerProbeSeconds           = int32(10)
	InitialContainerProbeSeconds           = int32(1)
)

type ProbeResult string

const (
	ProbeResultSuccess ProbeResult = "success"
	ProbeResultFailed  ProbeResult = "failed"
	ProbeResultUnknown ProbeResult = "unknown"
)

type ProbeStat struct {
	ConsecutiveFailures int32
	ConsecutiveSuccess  int32
}

type ContainerProber struct {
	probeResultFunc ProbeResultFunc
	stop            bool
	containerId     string
	podSpec         *v1.Pod
	runtimeService  runtime.RuntimeService
	Probe           *v1.Probe
	Container       *types.FornaxContainer
	ProbeType       ProbeType
	LastProbeTime   time.Time
	ProbeStat       ProbeStat
	Ticker          *time.Ticker
}

func (prober *ContainerProber) Stop() {
	prober.stop = true
}

func (prober *ContainerProber) Start() {
	go func() {
		for {
			select {
			case _ = <-prober.Ticker.C:
			}

			if prober.stop {
				prober.Ticker.Stop()
				break
			}

			obj, err := prober.ExecProbe()
			if err != nil {
				if prober.ProbeStat.ConsecutiveSuccess > 0 {
					prober.ProbeStat.ConsecutiveSuccess = 0
					prober.ProbeStat.ConsecutiveFailures = 1
				} else {
					prober.ProbeStat.ConsecutiveFailures += 1
				}
			} else {
				if prober.ProbeStat.ConsecutiveFailures > 0 {
					prober.ProbeStat.ConsecutiveFailures = 0
					prober.ProbeStat.ConsecutiveSuccess = 1
				} else {
					prober.ProbeStat.ConsecutiveSuccess += 1
				}
			}

			if prober.LastProbeTime.Unix() == 0 {
				// first probe, reset ticker to periodSeconds
				if prober.Probe.PeriodSeconds > 0 {
					prober.Ticker.Reset(time.Duration(prober.Probe.PeriodSeconds) * time.Second)
				}
			}

			var result ProbeResult
			if prober.ProbeStat.ConsecutiveFailures == 0 && prober.ProbeStat.ConsecutiveSuccess >= prober.Probe.SuccessThreshold {
				result = ProbeResultSuccess
			} else if prober.ProbeStat.ConsecutiveSuccess == 0 && prober.ProbeStat.ConsecutiveFailures >= prober.Probe.FailureThreshold {
				result = ProbeResultFailed
			} else {
				result = ProbeResultUnknown
			}

			msg := PodContainerProbeResult{
				Result:    result,
				ProbeType: prober.ProbeType,
			}

			prober.probeResultFunc(msg, obj)
		}
	}()
}

func (prober *ContainerProber) ExecProbe() (interface{}, error) {
	switch prober.ProbeType {
	case RuntimeStatusProbe:
		status, err := prober.runtimeService.GetContainerStatus(prober.containerId)
		if err != nil {
			return nil, err
		}
		if ContainerExit(status) {
			// do not probe anymore
			prober.Stop()
		} else if ContainerRunning(status) {
			// probe running container with less frequency
			prober.Ticker.Reset(time.Duration(RunningContainerProbeSeconds) * time.Second)
		}
		return status, nil
		// TODO
	case LivenessProbe:
	case ReadinessProbe:
	case StartupProbe:
	default:
	}
	return nil, nil
}

type ProbeResultFunc func(PodContainerProbeResult, interface{})

func NewContainerProber(probeResultFunc ProbeResultFunc, pod *v1.Pod, containerId string, probe *v1.Probe, probeType ProbeType, runtimeService runtime.RuntimeService) *ContainerProber {
	prober := &ContainerProber{
		stop:            false,
		podSpec:         pod,
		containerId:     containerId,
		probeResultFunc: probeResultFunc,
		ProbeType:       probeType,
		Probe:           probe,
		runtimeService:  runtimeService,
		LastProbeTime:   time.Unix(0, 0),
		ProbeStat:       ProbeStat{ConsecutiveFailures: 0, ConsecutiveSuccess: 0},
		Ticker:          time.NewTicker(time.Duration(probe.InitialDelaySeconds) * time.Second),
	}

	return prober
}

func NewRuntimeStatusProbeSpec() *v1.Probe {
	probe := &v1.Probe{
		ProbeHandler: v1.ProbeHandler{
			Exec:      nil,
			HTTPGet:   nil,
			TCPSocket: nil,
			GRPC:      nil,
		},
		InitialDelaySeconds: InitialContainerProbeSeconds,
		TimeoutSeconds:      10,
		PeriodSeconds:       InitialContainerProbeSeconds,
		SuccessThreshold:    1,
		FailureThreshold:    1,
	}
	return probe
}
