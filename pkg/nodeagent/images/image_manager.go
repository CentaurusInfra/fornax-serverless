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

package images

import (
	"fmt"
	"strings"

	dockerref "github.com/docker/distribution/reference"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	cri "k8s.io/cri-api/pkg/apis"
	criv1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type ImageManager interface {
	PullImageForContainer(container *v1.Container, podSandboxConfig *criv1.PodSandboxConfig) (*criv1.Image, error)
}

// imageManager provides the functionalities for image pulling.
type imageManager struct {
	imageRefs    map[string]*criv1.Image
	imageService cri.ImageManagerService
	authConfig   *criv1.AuthConfig
}

var _ ImageManager = &imageManager{}

func NewImageManager(imageService cri.ImageManagerService, authConfig *criv1.AuthConfig) ImageManager {
	return &imageManager{
		imageRefs:    map[string]*criv1.Image{},
		imageService: imageService,
		authConfig:   authConfig,
	}
}

func shouldPullImage(container *v1.Container, imagePresent bool) bool {
	if container.ImagePullPolicy == v1.PullNever {
		return false
	}

	if container.ImagePullPolicy == v1.PullAlways ||
		(container.ImagePullPolicy == v1.PullIfNotPresent && (!imagePresent)) {
		return true
	}

	return false
}

func (m *imageManager) PullImageForContainer(container *v1.Container, podSandboxConfig *criv1.PodSandboxConfig) (*criv1.Image, error) {
	imageWithTag, err := applyDefaultImageTag(container.Image)
	if err != nil {
		klog.ErrorS(err, "Failed to apply default image tag", container.Image)
		return nil, ErrInvalidImageName
	}

	image, found := m.imageRefs[imageWithTag]
	if found {
		klog.Infof("Container image with tag %s already present on machine", imageWithTag)
		return image, nil
	}

	imageSpec := &criv1.ImageSpec{
		Image: imageWithTag,
	}
	images, err := m.imageService.ListImages(&criv1.ImageFilter{
		Image: imageSpec,
	})
	if err != nil {
		klog.ErrorS(err, "Failed to list image", imageWithTag)
		return nil, ErrImageInspect
	}

	present := false
	for _, v := range images {
		for _, t := range v.GetRepoTags() {
			present = strings.HasSuffix(t, imageWithTag) || present
		}
		if present {
			image = v
			break
		}
	}

	if image != nil {
		m.imageRefs[imageWithTag] = image
		klog.InfoS("Container image already present on machine", "image", image, "tag", imageWithTag)
		return image, nil
	}

	_, err = m.imageService.PullImage(imageSpec, m.authConfig, podSandboxConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to pull image", "image", imageWithTag)
		return nil, ErrImagePull
	}

	images, err = m.imageService.ListImages(&criv1.ImageFilter{
		Image: imageSpec,
	})

	if err != nil {
		klog.ErrorS(err, "Failed to list image", "image", imageWithTag)
		return nil, ErrImageInspect
	}

	for _, v := range images {
		for _, t := range v.GetRepoTags() {
			present = strings.HasSuffix(t, imageWithTag) || present
		}
		if present {
			image = v
			m.imageRefs[imageWithTag] = image
			break
		}
	}

	return image, nil
}

// applyDefaultImageTag parses a docker image string, if it doesn't contain any tag or digest,
// a default tag will be applied.
func applyDefaultImageTag(image string) (string, error) {
	named, err := dockerref.ParseNormalizedNamed(image)
	if err != nil {
		return "", fmt.Errorf("couldn't parse image reference %q: %v", image, err)
	}
	_, isTagged := named.(dockerref.Tagged)
	_, isDigested := named.(dockerref.Digested)
	if !isTagged && !isDigested {
		// we just concatenate the image name with the default tag here instead
		// of using dockerref.WithTag(named, ...) because that would cause the
		// image to be fully qualified as docker.io/$name if it's a short name
		// (e.g. just busybox). We don't want that to happen to keep the CRI
		// agnostic wrt image names and default hostnames.
		image = image + ":latest"
	}
	return image, nil
}
