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
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	fornaxstore "centaurusinfra.io/fornax-serverless/pkg/store"
	storefactory "centaurusinfra.io/fornax-serverless/pkg/store/factory"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	apistorage "k8s.io/apiserver/pkg/storage"

	v1 "k8s.io/api/core/v1"
)

var _ ie.SessionManagerInterface = &sessionManager{}

type sessionManager struct {
	ctx             context.Context
	nodeAgentClient nodeagent.NodeAgentClient
	sessionStore    fornaxstore.ApiStorageInterface
}

func NewSessionManager(ctx context.Context, sessionStore fornaxstore.ApiStorageInterface, nodeAgentProxy nodeagent.NodeAgentClient) *sessionManager {
	mgr := &sessionManager{
		ctx:             ctx,
		nodeAgentClient: nodeAgentProxy,
		sessionStore:    sessionStore,
	}
	return mgr
}

// treat node as authority for session status, session status from node could be Starting, Available, Closed,
// use status from node to update storge status, session will be deleted if session is already closed
// if a session from node does not exist in pool, add it
func (sm *sessionManager) OnSessionStatusFromNode(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	storeCopy, err := storefactory.GetApplicationSessionCache(sm.sessionStore, util.Name(session))
	if err != nil {
		return err
	}
	if storeCopy == nil {
		// it should not happen as session from node should be created in store already, unless store corruption
		if !util.SessionIsClosed(session) {
			storefactory.CreateApplicationSession(sm.ctx, sm.sessionStore, session)
		}
	} else {
		if util.SessionIsOpen(session) && storeCopy.DeletionTimestamp != nil {
			// session was requested to delete, ask node to close session
			session.DeletionTimestamp = storeCopy.DeletionTimestamp
			sm.CloseSession(pod, session)
		}
		// set available and close time received in fornax core, for perf benchmark
		if session.Status.SessionStatus == fornaxv1.SessionStatusAvailable {
			session.Status.AvailableTime = util.NewCurrentMetaTimeNormallized()
			session.Status.AvailableTimeMicro = time.Now().UnixMicro()
		}
		if session.Status.SessionStatus == fornaxv1.SessionStatusClosed {
			session.Status.CloseTime = util.NewCurrentMetaTimeNormallized()
		}

		util.MergeObjectMeta(&session.ObjectMeta, &storeCopy.ObjectMeta)
		updatedSession := setSessionStatus(storeCopy, session.Status.DeepCopy())
		_, updateErr := storefactory.UpdateApplicationSession(sm.ctx, sm.sessionStore, updatedSession)
		return updateErr
	}

	return nil
}

func (sm *sessionManager) CloseSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetAnnotations()[fornaxv1.AnnotationFornaxCoreNode]; found {
		return sm.nodeAgentClient.CloseSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node this pod is on, %s", util.Name(pod))
	}
}

func (sm *sessionManager) OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetAnnotations()[fornaxv1.AnnotationFornaxCoreNode]; found {
		return sm.nodeAgentClient.OpenSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node session is on, %s", util.Name(session))
	}
}

// UpdateApplicationSessionStatus put updated status into a map send singal into a channel to asynchronously update session status
func (sm *sessionManager) Watch(ctx context.Context) (<-chan fornaxstore.WatchEventWithOldObj, error) {
	wi, err := sm.sessionStore.WatchWithOldObj(ctx, fornaxv1.ApplicationSessionGrvKey, apistorage.ListOptions{
		ResourceVersion:      "0",
		ResourceVersionMatch: "",
		Predicate:            apistorage.Everything,
		Recursive:            true,
		ProgressNotify:       true,
	})
	if err != nil {
		return nil, err
	}
	return wi.ResultChanWithPrevobj(), nil
}

// UpdateApplicationSessionStatus put updated status into a map send singal into a channel to asynchronously update session status
func (sm *sessionManager) UpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) error {
	sessionName := util.Name(session)
	var updateErr error
	for i := 0; i <= 3; i++ {
		session, err := storefactory.GetApplicationSessionCache(sm.sessionStore, sessionName)
		if err != nil {
			return err
		}
		if session == nil {
			return nil
		}

		updatedSession := session.DeepCopy()
		setSessionStatus(updatedSession, newStatus)
		_, updateErr = storefactory.UpdateApplicationSession(sm.ctx, sm.sessionStore, updatedSession)
		if updateErr == nil {
			break
		}
	}
	return updateErr
}

// attempts to update the Status of the given Application Session name
func setSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) *fornaxv1.ApplicationSession {
	session.Status = *newStatus
	if util.SessionIsOpen(session) {
		util.AddFinalizer(&session.ObjectMeta, fornaxv1.FinalizerOpenSession)
	} else {
		util.RemoveFinalizer(&session.ObjectMeta, fornaxv1.FinalizerOpenSession)
	}
	return session
}
