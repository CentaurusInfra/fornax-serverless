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

package util

import (
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
)

const (
	DefaultApplicationPodBurst                       = 2
	DefaultApplicationSesionDeleteGracePeriodSeconds = int64(5)
)

func ApplicationScalingBurst(app *fornaxv1.Application) int {
	if app.Spec.ScalingPolicy.Burst == 0 {
		return DefaultApplicationPodBurst
	}
	return int(app.Spec.ScalingPolicy.Burst)
}

func SessionIsOpen(session *fornaxv1.ApplicationSession) bool {
	return session.Status.SessionStatus != fornaxv1.SessionStatusUnspecified &&
		session.Status.SessionStatus != fornaxv1.SessionStatusPending &&
		session.Status.SessionStatus != fornaxv1.SessionStatusClosed &&
		session.Status.SessionStatus != fornaxv1.SessionStatusTimeout
}

func SessionIsClosed(session *fornaxv1.ApplicationSession) bool {
	return session.Status.SessionStatus == fornaxv1.SessionStatusClosed
}

func SessionIsClosing(session *fornaxv1.ApplicationSession) bool {
	return session.DeletionTimestamp != nil && session.Status.SessionStatus == fornaxv1.SessionStatusClosing
}

func SessionIsPending(session *fornaxv1.ApplicationSession) bool {
	return session.DeletionTimestamp == nil && (session.Status.SessionStatus == fornaxv1.SessionStatusPending || session.Status.SessionStatus == fornaxv1.SessionStatusUnspecified)
}

func SessionIsStarting(session *fornaxv1.ApplicationSession) bool {
	return session.Status.SessionStatus == fornaxv1.SessionStatusStarting
}

func SessionInTerminalState(session *fornaxv1.ApplicationSession) bool {
	return session.Status.SessionStatus == fornaxv1.SessionStatusClosed || session.Status.SessionStatus == fornaxv1.SessionStatusTimeout
}

func SessionInGracePeriod(session *fornaxv1.ApplicationSession) bool {
	gracefulseconds := DefaultApplicationSesionDeleteGracePeriodSeconds
	if session.DeletionGracePeriodSeconds != nil {
		gracefulseconds = *session.DeletionGracePeriodSeconds
	}

	cutoffTime := time.Now().Add(time.Duration(-1*gracefulseconds) * time.Second)
	return session.DeletionTimestamp != nil && session.DeletionTimestamp.After(cutoffTime)
}
