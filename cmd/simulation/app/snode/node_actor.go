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

package snode

import (
	"fmt"
	"sync"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxgrpc "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/fornaxcore"
	internal "centaurusinfra.io/fornax-serverless/pkg/nodeagent/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/node"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

type SimulationNodeActor struct {
	nodeMutex     sync.RWMutex
	stopCh        chan struct{}
	node          *node.FornaxNode
	state         node.NodeState
	innerActor    message.Actor
	fornoxCoreRef message.ActorRef
}

func (n *SimulationNodeActor) Stop() error {
	n.stopCh <- struct{}{}
	n.innerActor.Stop()
	return nil
}

func (n *SimulationNodeActor) Start() error {
	n.innerActor.Start()

	n.state = node.NodeStateRegistering
	var count int32
	n.incrementNodeRevision()
	// register with fornax core
	for {
		if n.state != node.NodeStateRegistering {
			// if node has received node configuration from fornaxcore
			break
		}
		klog.InfoS("Start node registry", "node spec", n.node.V1Node)
		messageType := fornaxgrpc.MessageType_NODE_REGISTER
		klog.Infof("Node Message Type %d : %s", count, messageType) //test purpose for this line
		n.notify(
			n.fornoxCoreRef,
			&fornaxgrpc.FornaxCoreMessage{
				MessageType: &messageType,
				MessageBody: &fornaxgrpc.FornaxCoreMessage_NodeRegistry{
					NodeRegistry: &fornaxgrpc.NodeRegistry{
						NodeRevision: &n.node.Revision,
						Node:         n.node.V1Node,
					},
				},
			},
		)
		time.Sleep(5 * time.Second)
		count++
	}

	return nil
}

func (n *SimulationNodeActor) startStateReport() {
	// start go routine to report node status forever
	go wait.Until(func() {
		SetNodeStatus(n.node)
		n.notify(n.fornoxCoreRef, node.BuildFornaxGrpcNodeState(n.node, n.node.Revision))
	}, 1*time.Minute, n.stopCh)
}

func (n *SimulationNodeActor) incrementNodeRevision() (int64, error) {
	n.nodeMutex.Lock()
	defer n.nodeMutex.Unlock()
	n.node.Revision += 1
	return n.node.Revision, nil
}

func (n *SimulationNodeActor) actorMessageProcess(msg message.ActorMessage) (interface{}, error) {
	switch msg.Body.(type) {
	case *fornaxgrpc.FornaxCoreMessage:
		return n.processFornaxCoreMessage(msg.Body.(*fornaxgrpc.FornaxCoreMessage))
	case internal.PodStatusChange:
		// increment node revision since one pod status changed
		fppod := msg.Body.(internal.PodStatusChange).Pod
		fppod.Pod.DeletionTimestamp = util.NewCurrentMetaTime()
		n.incrementNodeRevision()
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(n.node.Revision, fppod))
	case internal.PodCleanup:
		fppod := msg.Body.(internal.PodCleanup).Pod
		klog.InfoS("Cleanup pod actor and store", "pod", fornaxtypes.UniquePodName(fppod), "state", fppod.FornaxPodState)
		if fppod.FornaxPodState != fornaxtypes.PodStateTerminated && fppod.FornaxPodState != fornaxtypes.PodStateFailed {
			klog.Warning("Received cleanup when pod is not in terminated or failed state", "pod", fornaxtypes.UniquePodName(fppod))
			return nil, fmt.Errorf("Pod is not in terminated state, pod: %s", fornaxtypes.UniquePodName(fppod))
		}
		n.node.Pods.Del(fppod.Identifier)
	default:
		klog.InfoS("Received unknown message", "from", msg.Sender, "msg", msg.Body)
	}

	return nil, nil
}

// TODO, notify fornax core fatal error
func (n *SimulationNodeActor) processFornaxCoreMessage(msg *fornaxgrpc.FornaxCoreMessage) (interface{}, error) {
	var err error
	msgType := msg.GetMessageType()
	switch msgType {
	case fornaxgrpc.MessageType_NODE_CONFIGURATION:
		err = n.onNodeConfigurationCommand(msg.GetNodeConfiguration())
	case fornaxgrpc.MessageType_NODE_FULL_SYNC:
		err = n.onNodeFullSyncCommand(msg.GetNodeFullSync())
	case fornaxgrpc.MessageType_POD_CREATE:
		err = n.onPodCreateCommand(msg.GetPodCreate())
	case fornaxgrpc.MessageType_POD_TERMINATE:
		err = n.onPodTerminateCommand(msg.GetPodTerminate())
	case fornaxgrpc.MessageType_POD_ACTIVE:
		err = n.onPodActiveCommand(msg.GetPodActive())
	case fornaxgrpc.MessageType_SESSION_START:
		err = n.onSessionStartCommand(msg.GetSessionStart())
	case fornaxgrpc.MessageType_SESSION_CLOSE:
		err = n.onSessionCloseCommand(msg.GetSessionClose())
	case fornaxgrpc.MessageType_SESSION_STATE, fornaxgrpc.MessageType_POD_STATE, fornaxgrpc.MessageType_NODE_STATE:
		// messages are sent to fornaxcore, should just forward
		n.notify(n.fornoxCoreRef, msg)
	default:
		// messages are not supposed to be received by node
		klog.Warningf("FornaxCoreMessage of type %s is not handled by actor", msgType)
	}
	// TODO, handle error, define reply message to fornax core
	return nil, err
}

// initialize node with node spec provided by fornaxcore, especially pod cidr
func (n *SimulationNodeActor) onNodeFullSyncCommand(msg *fornaxgrpc.NodeFullSync) error {
	n.notify(n.fornoxCoreRef, node.BuildFornaxGrpcNodeState(n.node, n.node.Revision))
	return nil
}

// initialize node with node spec provided by fornaxcore, especially pod cidr
func (n *SimulationNodeActor) onNodeConfigurationCommand(msg *fornaxgrpc.NodeConfiguration) error {
	if n.state != node.NodeStateRegistering {
		return fmt.Errorf("node is not in registering state, it does not expect configuration change after registering")
	}

	apiNode := msg.GetNode()
	n.node.V1Node.Spec = *apiNode.Spec.DeepCopy()

	err := n.initializeNodeDaemons(msg.DaemonPods)
	if err != nil {
		klog.ErrorS(err, "Failed to initiaize daemons")
		return err
	}

	n.state = node.NodeStateRegistered
	// start go routine to check node status until it is ready
	go func() {
		for {
			// finish if node has initialized
			if n.state != node.NodeStateRegistered {
				break
			}

			// check node runtime dependencies, send node ready message if node is ready for new pod
			SetNodeStatus(n.node)
			if IsNodeStatusReady(n.node) {
				klog.InfoS("Node is ready, tell fornax core")
				// bump revision to let fornax core to update node status
				revision, err := n.incrementNodeRevision()
				if err == nil {
					n.node.V1Node.Status.Phase = v1.NodeRunning
					n.notify(n.fornoxCoreRef, node.BuildFornaxGrpcNodeReady(n.node, revision))
					n.state = node.NodeStateReady
					n.startStateReport()
				}
			} else {
				time.Sleep(5 * time.Second)
			}
		}
	}()
	return nil
}

func (n *SimulationNodeActor) initializeNodeDaemons(pods []*v1.Pod) error {
	for _, p := range pods {
		klog.Infof("Initialize daemon pod, %v", p)
		errs := pod.ValidatePodSpec(p)
		if len(errs) != 0 {
			return errors.Errorf("Pod spec is invalid %v", errs)
		}

		if len(p.Spec.Containers) != 1 {
			return errors.Errorf("Daemon pod can only have one container, but this pod spec has %d container(s)", len(p.Spec.Containers))
		}

		if !p.Spec.HostNetwork {
			return errors.Errorf("Daemon pod must use host network")
		}

		v := n.node.Pods.Get(util.UniquePodName(p))
		if v == nil {
			_, err := n.createPodAndActor(fornaxtypes.PodStateRunning, p.DeepCopy(), nil, true)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *SimulationNodeActor) buildAFornaxPod(state fornaxtypes.PodState,
	v1pod *v1.Pod,
	configMap *v1.ConfigMap,
	isDaemon bool) (*fornaxtypes.FornaxPod, error) {
	errs := pod.ValidatePodSpec(v1pod)
	if len(errs) > 0 {
		return nil, errors.New("Pod spec is invalid")
	}
	fornaxPod := &fornaxtypes.FornaxPod{
		Identifier:              util.UniquePodName(v1pod),
		FornaxPodState:          state,
		Daemon:                  isDaemon,
		Pod:                     v1pod.DeepCopy(),
		RuntimePod:              nil,
		Containers:              map[string]*fornaxtypes.FornaxContainer{},
		LastStateTransitionTime: time.Now(),
	}
	pod.SetPodStatus(fornaxPod, n.node.V1Node)
	fornaxPod.Pod.Labels[fornaxv1.LabelFornaxCoreNode] = util.UniqueNodeName(n.node.V1Node)

	if configMap != nil {
		errs = pod.ValidateConfigMapSpec(configMap)
		if len(errs) > 0 {
			return nil, errors.New("ConfigMap spec is invalid")
		}
		fornaxPod.ConfigMap = configMap.DeepCopy()
	}
	return fornaxPod, nil
}

func (n *SimulationNodeActor) createPodAndActor(state fornaxtypes.PodState,
	v1Pod *v1.Pod,
	v1Config *v1.ConfigMap,
	isDaemon bool) (*fornaxtypes.FornaxPod, error) {
	// create fornax pod obj
	fpod, err := n.buildAFornaxPod(state, v1Pod, v1Config, isDaemon)
	if err != nil {
		klog.ErrorS(err, "Failed to build a FornaxPod from pod spec", "namespace", v1Pod.Namespace, "name", v1Pod.Name)
		return nil, err
	}

	n.node.Pods.Add(fpod.Identifier, fpod)
	return fpod, nil
}

// find pod actor and send a message to it, if pod actor does not exist, create one
func (n *SimulationNodeActor) onPodCreateCommand(msg *fornaxgrpc.PodCreate) error {
	if n.state != node.NodeStateReady {
		return fmt.Errorf("Node is not in ready state to create a new pod")
	}
	v := n.node.Pods.Get(*msg.PodIdentifier)
	if v == nil {
		fpod, err := n.createPodAndActor(
			fornaxtypes.PodStateCreating,
			msg.GetPod().DeepCopy(),
			msg.GetConfigMap().DeepCopy(),
			false,
		)
		if err != nil {
			return err
		}
		fpod.Pod.Status.Phase = v1.PodRunning

		conditions := []v1.PodCondition{}
		initReadyCondition := v1.PodCondition{
			Type:          v1.PodInitialized,
			Status:        v1.ConditionTrue,
			LastProbeTime: *util.NewCurrentMetaTime(),
		}
		conditions = append(conditions, initReadyCondition)

		containerReadyCondition := v1.PodCondition{
			Type:          v1.ContainersReady,
			Status:        v1.ConditionTrue,
			LastProbeTime: *util.NewCurrentMetaTime(),
		}
		conditions = append(conditions, containerReadyCondition)

		podReadyCondition := v1.PodCondition{
			Type:          v1.PodReady,
			Status:        v1.ConditionTrue,
			LastProbeTime: *util.NewCurrentMetaTime(),
		}
		conditions = append(conditions, podReadyCondition)

		podScheduledCondition := v1.PodCondition{
			Type:          v1.PodScheduled,
			Status:        v1.ConditionTrue,
			LastProbeTime: *util.NewCurrentMetaTime(),
		}
		conditions = append(conditions, podScheduledCondition)
		fpod.Pod.Status.Conditions = conditions

		klog.Info("Pod have been created, ", "Pod Identifier ID: ", fpod.Identifier)
		time.Sleep(1 * time.Second)
		fpod.FornaxPodState = fornaxtypes.PodStateRunning
		n.incrementNodeRevision()
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(n.node.Revision, fpod))
	} else {
		// TODO, need to update daemon if spec changed
		// not supposed to receive create command for a existing pod, ignore it and send back pod status
		// pod should be terminate and recreated for update case
		return fmt.Errorf("Pod: %s already exist", *msg.PodIdentifier)
	}
	return nil
}

// find pod actor and send a message to it, if pod actor does not exist, return error
func (n *SimulationNodeActor) onPodTerminateCommand(msg *fornaxgrpc.PodTerminate) error {
	fpod := n.node.Pods.Get(*msg.PodIdentifier)
	if fpod == nil {
		return fmt.Errorf("Pod: %s does not exist, fornax core is not in sync", *msg.PodIdentifier)
	} else {
		fpod.Pod.Status.Phase = v1.PodSucceeded
		fpod.FornaxPodState = fornaxtypes.PodStateTerminated
		klog.Info("Pod will be terminated and Pod ID: ", fpod.Identifier)
		time.Sleep(1 * time.Second)
		n.incrementNodeRevision()
		n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(n.node.Revision, fpod))
		n.node.Pods.Del(*msg.PodIdentifier)
		klog.Info("Pod have been terminated and Pod ID: ", fpod.Identifier)
	}
	return nil
}

// find pod actor and send a message to it, if pod actor does not exist, return error
func (n *SimulationNodeActor) onPodActiveCommand(msg *fornaxgrpc.PodActive) error {
	panic("not implemented")
}

// find pod actor to let it start a session, if pod actor does not exist, return failure
func (n *SimulationNodeActor) onSessionStartCommand(msg *fornaxgrpc.SessionStart) error {
	panic("not implemented")
}

// find pod actor to let it terminate a session, if pod actor does not exist, return failure
func (n *SimulationNodeActor) onSessionCloseCommand(msg *fornaxgrpc.SessionClose) error {
	panic("not implemented")
}

func (n *SimulationNodeActor) notify(receiver message.ActorRef, msg interface{}) {
	message.Send(n.innerActor.Reference(), receiver, msg)
}

func NewNodeActor(hostIp, hostName string, fpnode *node.FornaxNode) (*SimulationNodeActor, error) {
	if fpnode.V1Node == nil {
		v1node, err := InitV1Node(hostIp, hostName)
		if err != nil {
			return nil, err
		} else {
			fpnode.V1Node = v1node
		}
	}
	SetNodeStatus(fpnode)

	actor := &SimulationNodeActor{
		nodeMutex:     sync.RWMutex{},
		stopCh:        make(chan struct{}),
		node:          fpnode,
		state:         node.NodeStateInitializing,
		innerActor:    nil,
		fornoxCoreRef: nil,
	}
	actor.innerActor = message.NewLocalChannelActor(fpnode.V1Node.GetName(), actor.actorMessageProcess)

	klog.Info("Starting FornaxCore actor")
	fornaxCoreActor := fornaxcore.NewFornaxCoreActor(fpnode.NodeConfig.NodeIP, fpnode.V1Node.Name, fpnode.NodeConfig.FornaxCoreIPs)
	actor.fornoxCoreRef = fornaxCoreActor.Reference()
	err := fornaxCoreActor.Start(actor.innerActor.Reference())
	if err != nil {
		klog.ErrorS(err, "Can not start fornax core actor")
		return nil, err
	}
	klog.Info("Fornax core actor started")

	return actor, nil
}
