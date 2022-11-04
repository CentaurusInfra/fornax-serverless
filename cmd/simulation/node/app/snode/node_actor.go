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
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	"centaurusinfra.io/fornax-serverless/cmd/simulation/node/config"
	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxgrpc "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc"
	"centaurusinfra.io/fornax-serverless/pkg/message"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/fornaxcore"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/node"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/session"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	nodetypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

type SimulationNodeActor struct {
	podsConcurrency *semaphore.Weighted
	nodeMutex       sync.RWMutex
	stopCh          chan struct{}
	node            *node.FornaxNode
	state           node.NodeState
	innerActor      message.Actor
	fornoxCoreRef   message.ActorRef
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
		n.notify(
			n.fornoxCoreRef,
			&fornaxgrpc.FornaxCoreMessage{
				MessageType: messageType,
				MessageBody: &fornaxgrpc.FornaxCoreMessage_NodeRegistry{
					NodeRegistry: &fornaxgrpc.NodeRegistry{
						NodeRevision: n.node.Revision,
						Node:         n.node.V1Node,
					},
				},
			},
		)
		time.Sleep(1 * time.Second)
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

func (n *SimulationNodeActor) incrementNodeRevision() int64 {
	revision := atomic.AddInt64(&n.node.Revision, 1)
	n.node.V1Node.ResourceVersion = fmt.Sprint(revision)
	return revision
}

func (n *SimulationNodeActor) actorMessageProcess(msg message.ActorMessage) (interface{}, error) {
	switch msg.Body.(type) {
	case *fornaxgrpc.FornaxCoreMessage:
		return n.processFornaxCoreMessage(msg.Body.(*fornaxgrpc.FornaxCoreMessage))
	default:
		klog.InfoS("Received unknown message", "from", msg.Sender, "msg", msg.Body, "node", n.node.V1Node.Name)
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
		go func() {
			n.podsConcurrency.Acquire(context.Background(), 1)
			defer n.podsConcurrency.Release(1)
			n.onPodCreateCommand(msg.GetPodCreate())
		}()
	case fornaxgrpc.MessageType_POD_TERMINATE:
		go func() {
			n.podsConcurrency.Acquire(context.Background(), 1)
			defer n.podsConcurrency.Release(1)
			n.onPodTerminateCommand(msg.GetPodTerminate())
		}()
	case fornaxgrpc.MessageType_POD_HIBERNATE:
		err = n.onPodHibernateCommand(msg.GetPodHibernate())
	case fornaxgrpc.MessageType_SESSION_OPEN:
		go func() {
			n.onSessionOpenCommand(msg.GetSessionOpen())
		}()
	case fornaxgrpc.MessageType_SESSION_CLOSE:
		go func() {
			n.onSessionCloseCommand(msg.GetSessionClose())
		}()
	case fornaxgrpc.MessageType_SESSION_STATE, fornaxgrpc.MessageType_POD_STATE, fornaxgrpc.MessageType_NODE_STATE:
		// messages are sent to fornaxcore, should just forward
		n.notify(n.fornoxCoreRef, msg)
	default:
		// messages are not supposed to be received by node
		klog.InfoS("FornaxCoreMessage is not recognized by actor", "msgType", msgType, "node", n.node.V1Node.Name)
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
		klog.ErrorS(err, "Failed to initiaize daemons", "node", n.node.V1Node.Name)
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
				klog.InfoS("Node is ready, tell fornax core", "node", n.node.V1Node.Name)
				// bump revision to let fornax core to update node status
				func() {
					n.nodeMutex.Lock()
					defer n.nodeMutex.Unlock()
					revision := n.incrementNodeRevision()
					n.node.V1Node.Status.Phase = v1.NodeRunning
					n.notify(n.fornoxCoreRef, node.BuildFornaxGrpcNodeReady(n.node, revision))
				}()
				n.state = node.NodeStateReady
				n.startStateReport()
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}()
	return nil
}

func (n *SimulationNodeActor) initializeNodeDaemons(pods []*v1.Pod) error {
	for _, p := range pods {
		klog.InfoS("Initialize daemon pod", "pod", p, "node", n.node.V1Node.Name)
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

		v := n.node.Pods.Get(util.Name(p))
		if v == nil {
			_, err := n.createPodAndActor(nodetypes.PodStateRunning, p.DeepCopy(), nil, true)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *SimulationNodeActor) buildAFornaxPod(state nodetypes.PodState,
	v1pod *v1.Pod,
	configMap *v1.ConfigMap,
	isDaemon bool) (*nodetypes.FornaxPod, error) {
	errs := pod.ValidatePodSpec(v1pod)
	if len(errs) > 0 {
		return nil, errors.New("Pod spec is invalid")
	}
	fornaxPod := &nodetypes.FornaxPod{
		Identifier:              util.Name(v1pod),
		FornaxPodState:          state,
		Daemon:                  isDaemon,
		Pod:                     v1pod.DeepCopy(),
		ConfigMap:               configMap,
		RuntimePod:              nil,
		Containers:              map[string]*nodetypes.FornaxContainer{},
		Sessions:                map[string]*nodetypes.FornaxSession{},
		LastStateTransitionTime: time.Now(),
	}
	pod.SetPodStatus(fornaxPod, n.node.V1Node)
	fornaxPod.Pod.Labels[fornaxv1.LabelFornaxCoreNode] = util.Name(n.node.V1Node)

	if configMap != nil {
		errs = pod.ValidateConfigMapSpec(configMap)
		if len(errs) > 0 {
			return nil, errors.New("ConfigMap spec is invalid")
		}
		fornaxPod.ConfigMap = configMap.DeepCopy()
	}
	return fornaxPod, nil
}

func (n *SimulationNodeActor) createPodAndActor(state nodetypes.PodState,
	v1Pod *v1.Pod,
	v1Config *v1.ConfigMap,
	isDaemon bool) (*nodetypes.FornaxPod, error) {
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
	klog.InfoS("Creating Pod", "pod", msg.PodIdentifier, "node", n.node.V1Node.Name)
	if n.state != node.NodeStateReady {
		return fmt.Errorf("Node is not in ready state to create a new pod")
	}
	v := n.node.Pods.Get(msg.GetPodIdentifier())
	if v == nil {
		fpod, err := n.createPodAndActor(
			nodetypes.PodStateCreating,
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

		time.Sleep(1 * time.Second)
		fpod.FornaxPodState = nodetypes.PodStateRunning
		func() {
			n.nodeMutex.Lock()
			defer n.nodeMutex.Unlock()
			revision := n.incrementNodeRevision()
			fpod.Pod.ResourceVersion = fmt.Sprint(revision)
			klog.InfoS("Pod have been created", "pod", fpod.Identifier, "node", n.node.V1Node.Name)
			n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(revision, fpod))
		}()
	} else {
		return fmt.Errorf("Pod: %s already exist", msg.GetPodIdentifier())
	}
	return nil
}

// find pod actor and send a message to it, if pod actor does not exist, return error
func (n *SimulationNodeActor) onPodTerminateCommand(msg *fornaxgrpc.PodTerminate) error {
	klog.InfoS("Terminating Pod", "pod", msg.PodIdentifier, "node", n.node.V1Node.Name)
	fpod := n.node.Pods.Get(msg.GetPodIdentifier())
	if fpod == nil {
		return fmt.Errorf("Pod: %s does not exist, fornax core is not in sync", msg.GetPodIdentifier())
	} else {
		time.Sleep(1 * time.Second)
		fpod.Pod.Status.Phase = v1.PodSucceeded
		fpod.FornaxPodState = nodetypes.PodStateTerminated
		func() {
			n.nodeMutex.Lock()
			defer n.nodeMutex.Unlock()
			revision := n.incrementNodeRevision()
			fpod.Pod.ResourceVersion = fmt.Sprint(revision)
			n.notify(n.fornoxCoreRef, pod.BuildFornaxcoreGrpcPodState(revision, fpod))
		}()
		n.node.Pods.Del(msg.GetPodIdentifier())
		klog.InfoS("Pod have been terminated", "pod", fpod.Identifier, "node", n.node.V1Node.Name)
	}
	return nil
}

// find pod actor and send a message to it, if pod actor does not exist, return error
func (n *SimulationNodeActor) onPodHibernateCommand(msg *fornaxgrpc.PodHibernate) error {
	panic("not implemented")
}

// find pod actor to let it open a session, if pod actor does not exist, return failure
func (n *SimulationNodeActor) onSessionOpenCommand(msg *fornaxgrpc.SessionOpen) error {
	klog.InfoS("Opening session", "session", msg.SessionIdentifier, "pod", msg.PodIdentifier, "node", n.node.V1Node.Name)
	if n.state != node.NodeStateReady {
		return fmt.Errorf("node is not in ready state to open a session")
	}
	sess := &fornaxv1.ApplicationSession{}
	if err := json.Unmarshal(msg.GetSessionData(), sess); err != nil {
		return err
	}
	fpod := n.node.Pods.Get(msg.GetPodIdentifier())
	if fpod == nil {
		return fmt.Errorf("Pod: %s does not exist, can not open session", msg.GetPodIdentifier())
	} else {
		sessId := util.Name(sess)
		fsess := &types.FornaxSession{
			Identifier:     sessId,
			PodIdentifier:  fpod.Identifier,
			Session:        sess,
			ClientSessions: map[string]*nodetypes.ClientSession{},
		}
		time.Sleep(3 * time.Millisecond)
		func() {
			n.nodeMutex.Lock()
			defer n.nodeMutex.Unlock()
			revision := n.incrementNodeRevision()
			sess.ResourceVersion = fmt.Sprint(revision)
			fpod.Sessions[sessId] = fsess
			fsess.Session.Status.SessionStatus = fornaxv1.SessionStatusAvailable
			n.notify(n.fornoxCoreRef, session.BuildFornaxcoreGrpcSessionState(revision, fsess))
			klog.InfoS("Opened session", "session", msg.SessionIdentifier, "pod", msg.PodIdentifier, "node", n.node.V1Node.Name)
		}()
	}
	return nil
}

// find pod actor to let it terminate a session, if pod actor does not exist, return failure
func (n *SimulationNodeActor) onSessionCloseCommand(msg *fornaxgrpc.SessionClose) error {
	klog.InfoS("Closing session", "session", msg.SessionIdentifier, "pod", msg.PodIdentifier, "node", n.node.V1Node.Name)
	sessId := msg.GetSessionIdentifier()
	podId := msg.GetPodIdentifier()
	fpod := n.node.Pods.Get(podId)
	if fpod == nil {
		return fmt.Errorf("Pod: %s does not exist, can not terminate session", podId)
	} else {
		if fsess, found := fpod.Sessions[sessId]; found {
			time.Sleep(3 * time.Millisecond)
			func() {
				n.nodeMutex.Lock()
				defer n.nodeMutex.Unlock()
				revision := n.incrementNodeRevision()
				fsess.Session.ResourceVersion = fmt.Sprint(revision)
				fsess.Session.Status.SessionStatus = fornaxv1.SessionStatusClosed
				n.notify(n.fornoxCoreRef, session.BuildFornaxcoreGrpcSessionState(revision, fsess))
				klog.InfoS("Closed session", "session", msg.SessionIdentifier, "pod", msg.PodIdentifier, "node", n.node.V1Node.Name)
			}()
		} else {
			return fmt.Errorf("Session: %s does not exist, can not terminate session", sessId)
		}
	}
	return nil

}

func (n *SimulationNodeActor) notify(receiver message.ActorRef, msg interface{}) {
	message.Send(n.innerActor.Reference(), receiver, msg)
}

func NewNodeActor(hostIp, hostName string, nodeConfig *config.SimulationNodeConfiguration) (*SimulationNodeActor, error) {
	v1node, err := InitV1Node(hostIp, hostName)
	if err != nil {
		return nil, err
	}
	fpnode := &node.FornaxNode{
		NodeConfig:   nodeConfig.NodeConfig,
		V1Node:       v1node,
		Pods:         node.NewPodPool(),
		Dependencies: nil,
	}
	SetNodeStatus(fpnode)

	actor := &SimulationNodeActor{
		podsConcurrency: semaphore.NewWeighted(int64(nodeConfig.PodConcurrency)),
		nodeMutex:       sync.RWMutex{},
		stopCh:          make(chan struct{}),
		node:            fpnode,
		state:           node.NodeStateInitializing,
		innerActor:      nil,
		fornoxCoreRef:   nil,
	}
	actor.innerActor = message.NewLocalChannelActor(fpnode.V1Node.GetName(), actor.actorMessageProcess)

	klog.InfoS("Starting FornaxCore actor", "node", hostName)
	fornaxCoreActor := fornaxcore.NewFornaxCoreActor(fpnode.NodeConfig.NodeIP, util.Name(fpnode.V1Node), nodeConfig.FornaxCoreUrls)
	actor.fornoxCoreRef = fornaxCoreActor.Reference()
	err = fornaxCoreActor.Start(actor.innerActor.Reference())
	if err != nil {
		klog.ErrorS(err, "Can not start fornax core actor", "node", hostName)
		return nil, err
	}
	klog.InfoS("Fornax core actor started", "node", hostName)

	return actor, nil
}
