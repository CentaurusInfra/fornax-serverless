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

package v1

const (
	LabelFornaxCoreNodeDaemon              = "daemon.fornax-serverless.centaurusinfra.io"
	LabelFornaxCoreApplication             = "application.core.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCoreNode               = "node.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCorePod                = "pod.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCoreCreationUnixMicro  = "create.unixmicro.core.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCoreSessionService     = "sessionservice.core.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCoreApplicationSession = "applicationsession.core.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCoreNodeRevision       = "noderevision.core.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCoreHibernatePod       = "hibernatepod.core.fornax-serverless.centaurusinfra.io"
	AnnotationFornaxCoreSessionServicePod  = "sessionservicepod.core.fornax-serverless.centaurusinfra.io"
)
