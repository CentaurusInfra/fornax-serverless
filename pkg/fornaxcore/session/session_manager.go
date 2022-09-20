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

package session

import (
	"context"
	"fmt"
	"reflect"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

var _ ie.SessionManagerInterface = &sessionManager{}

type sessionStatus struct {
	sessionName string
	status      *fornaxv1.ApplicationSessionStatus
}

type sessionManager struct {
	ctx             context.Context
	nodeAgentClient nodeagent.NodeAgentClient
	podManager      ie.PodManagerInterface
	watchers        []chan<- interface{}
	statusUpdateCh  chan *sessionStatus
	sessionStatus   map[string]*sessionStatus
}

func NewSessionManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentClient, podManager ie.PodManagerInterface) *sessionManager {
	mgr := &sessionManager{
		ctx:             ctx,
		nodeAgentClient: nodeAgentProxy,
		podManager:      podManager,
		statusUpdateCh:  make(chan *sessionStatus, 500),
		sessionStatus:   map[string]*sessionStatus{},
	}
	return mgr
}

// Run receive session status from channel and update session status use api service client
func (sm *sessionManager) Run(ctx context.Context) {
	klog.Info("Starting fornaxv1 session status manager")

	go func() {
		defer klog.Info("Shutting down fornaxv1 session status manager")

		FornaxCore_SessionStatusManager_Retry := "FornaxCore_SessionStatusManager_StatusUpdate_Retry"
		for {
			select {
			case <-ctx.Done():
				break
			case update := <-sm.statusUpdateCh:
				sm.sessionStatus[update.sessionName] = update
				remainingLen := len(sm.statusUpdateCh)
				for i := 0; i < remainingLen; i++ {
					update := <-sm.statusUpdateCh
					sm.sessionStatus[update.sessionName] = update
				}

				hasError := false
				updatedSessions := []string{}
				for session, status := range sm.sessionStatus {
					if session == FornaxCore_SessionStatusManager_Retry {
						continue
					}
					err := sm.updateSessionStatus(status.sessionName, status.status)
					if err == nil {
						updatedSessions = append(updatedSessions, session)
					} else {
						hasError = true
						klog.ErrorS(err, "Failed to update session status", "session", session)
					}
				}
				for _, v := range updatedSessions {
					delete(sm.sessionStatus, v)
				}

				// a trick to retry, all failed status update are still in map, send a fake update to retry,
				// it's bit risky, if some guy put a lot of event into channel before we can put a retry signal, it will stuck
				// checking channel current length must be zero could mitigate a bit
				if hasError {
					time.Sleep(50 * time.Millisecond)
					if len(sm.statusUpdateCh) == 0 {
						sm.statusUpdateCh <- &sessionStatus{
							sessionName: FornaxCore_SessionStatusManager_Retry,
							status:      &fornaxv1.ApplicationSessionStatus{},
						}
					}
				}
			}
		}
	}()
}

func (sm *sessionManager) Watch(receiver chan<- interface{}) {
	sm.watchers = append(sm.watchers, receiver)
}

// forward to who are interested in session event
func (sm *sessionManager) ReceiveSessionStatusFromNode(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) {
	for _, session := range sessions {
		for _, v := range sm.watchers {
			v <- &ie.SessionEvent{
				NodeId:  nodeId,
				Pod:     pod,
				Session: session,
				Type:    ie.SessionEventTypeUpdate,
			}
		}
	}
}

func (sm *sessionManager) CloseSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreNode]; found {
		return sm.nodeAgentClient.CloseSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node this pod is on, %s", util.Name(pod))
	}
}

func (sm *sessionManager) OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreNode]; found {
		return sm.nodeAgentClient.OpenSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node session is on, %s", util.Name(session))
	}
}

// UpdateApplicationSessionStatus send status update into a channel to asynchronously update session status
func (sm *sessionManager) UpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) error {
	sm.statusUpdateCh <- &sessionStatus{
		sessionName: util.Name(session),
		status:      newStatus,
	}
	return nil
}

// updateApplicationSessionStatus attempts to update the Status of the given Application Session
func (sm *sessionManager) updateSessionStatus(sessionName string, newStatus *fornaxv1.ApplicationSessionStatus) error {
	apiServerClient := util.GetFornaxCoreApiClient()
	namespace, name, _ := cache.SplitMetaNamespaceKey(sessionName)
	session, err := apiServerClient.CoreV1().ApplicationSessions(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if reflect.DeepEqual(session.Status, *newStatus) {
		return nil
	}

	updatedSession := session.DeepCopy()
	updatedSession.Status = *newStatus
	client := apiServerClient.CoreV1().ApplicationSessions(session.Namespace)
	for i := 0; i <= 3; i++ {
		updatedSession, err = client.UpdateStatus(context.TODO(), updatedSession, metav1.UpdateOptions{})
		if err == nil {
			break
		} else {
			updatedSession = session.DeepCopy()
			updatedSession.Status = *newStatus
		}
	}
	if err != nil {
		klog.ErrorS(err, "Failed to update session status", "sess", session)
		return err
	}

	return nil
}

func (sm *sessionManager) UpdateSessionFinalizer(session *fornaxv1.ApplicationSession) error {
	apiServerClient := util.GetFornaxCoreApiClient()
	client := apiServerClient.CoreV1().ApplicationSessions(session.Namespace)
	updatedSession := session.DeepCopy()
	if util.SessionIsOpen(updatedSession) {
		util.AddFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
	} else {
		util.RemoveFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
	}

	if len(session.Finalizers) != len(updatedSession.Finalizers) {
		for i := 0; i <= 3; i++ {
			_, err := client.Update(context.TODO(), updatedSession, metav1.UpdateOptions{})
			if err == nil {
				break
			}
		}
	}

	return nil
}
