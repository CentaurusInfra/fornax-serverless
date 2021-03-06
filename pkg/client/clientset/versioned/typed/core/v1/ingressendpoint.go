/*
Copyright 2022 The fornax-serverless Authors.

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
// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	"time"

	v1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	scheme "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// IngressEndpointsGetter has a method to return a IngressEndpointInterface.
// A group's client should implement this interface.
type IngressEndpointsGetter interface {
	IngressEndpoints(namespace string) IngressEndpointInterface
}

// IngressEndpointInterface has methods to work with IngressEndpoint resources.
type IngressEndpointInterface interface {
	Create(ctx context.Context, ingressEndpoint *v1.IngressEndpoint, opts metav1.CreateOptions) (*v1.IngressEndpoint, error)
	Update(ctx context.Context, ingressEndpoint *v1.IngressEndpoint, opts metav1.UpdateOptions) (*v1.IngressEndpoint, error)
	UpdateStatus(ctx context.Context, ingressEndpoint *v1.IngressEndpoint, opts metav1.UpdateOptions) (*v1.IngressEndpoint, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.IngressEndpoint, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.IngressEndpointList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.IngressEndpoint, err error)
	IngressEndpointExpansion
}

// ingressEndpoints implements IngressEndpointInterface
type ingressEndpoints struct {
	client rest.Interface
	ns     string
}

// newIngressEndpoints returns a IngressEndpoints
func newIngressEndpoints(c *CoreV1Client, namespace string) *ingressEndpoints {
	return &ingressEndpoints{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the ingressEndpoint, and returns the corresponding ingressEndpoint object, and an error if there is any.
func (c *ingressEndpoints) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.IngressEndpoint, err error) {
	result = &v1.IngressEndpoint{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("ingressendpoints").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of IngressEndpoints that match those selectors.
func (c *ingressEndpoints) List(ctx context.Context, opts metav1.ListOptions) (result *v1.IngressEndpointList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.IngressEndpointList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("ingressendpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested ingressEndpoints.
func (c *ingressEndpoints) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("ingressendpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a ingressEndpoint and creates it.  Returns the server's representation of the ingressEndpoint, and an error, if there is any.
func (c *ingressEndpoints) Create(ctx context.Context, ingressEndpoint *v1.IngressEndpoint, opts metav1.CreateOptions) (result *v1.IngressEndpoint, err error) {
	result = &v1.IngressEndpoint{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("ingressendpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(ingressEndpoint).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a ingressEndpoint and updates it. Returns the server's representation of the ingressEndpoint, and an error, if there is any.
func (c *ingressEndpoints) Update(ctx context.Context, ingressEndpoint *v1.IngressEndpoint, opts metav1.UpdateOptions) (result *v1.IngressEndpoint, err error) {
	result = &v1.IngressEndpoint{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("ingressendpoints").
		Name(ingressEndpoint.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(ingressEndpoint).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *ingressEndpoints) UpdateStatus(ctx context.Context, ingressEndpoint *v1.IngressEndpoint, opts metav1.UpdateOptions) (result *v1.IngressEndpoint, err error) {
	result = &v1.IngressEndpoint{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("ingressendpoints").
		Name(ingressEndpoint.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(ingressEndpoint).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the ingressEndpoint and deletes it. Returns an error if one occurs.
func (c *ingressEndpoints) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("ingressendpoints").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *ingressEndpoints) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("ingressendpoints").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched ingressEndpoint.
func (c *ingressEndpoints) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.IngressEndpoint, err error) {
	result = &v1.IngressEndpoint{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("ingressendpoints").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
