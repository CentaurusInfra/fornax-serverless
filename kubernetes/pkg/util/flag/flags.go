/*
Copyright 2018 The Kubernetes Authors.

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

package flag

import (
	"fmt"
	"net"
	"strings"

	"github.com/spf13/pflag"

	v1 "k8s.io/api/core/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"

	// kubeletconfig "k8s.io/kubernetes/pkg/kubelet/apis/config"
	utiltaints "k8s.io/kubernetes/pkg/util/taints"
	netutils "k8s.io/utils/net"
)

// TODO(mikedanese): remove these flag wrapper types when we remove command line flags

var (
	_ pflag.Value = &IPVar{}
	_ pflag.Value = &IPPortVar{}
	_ pflag.Value = &PortRangeVar{}
	_ pflag.Value = &RegisterWithTaintsVar{}
)

// IPVar is used for validating a command line option that represents an IP. It implements the pflag.Value interface
type IPVar struct {
	Val *string
}

// Set sets the flag value
func (v *IPVar) Set(s string) error {
	if len(s) == 0 {
		v.Val = nil
		return nil
	}
	if netutils.ParseIPSloppy(s) == nil {
		return fmt.Errorf("%q is not a valid IP address", s)
	}
	if v.Val == nil {
		// it's okay to panic here since this is programmer error
		panic("the string pointer passed into IPVar should not be nil")
	}
	*v.Val = s
	return nil
}

// String returns the flag value
func (v *IPVar) String() string {
	if v.Val == nil {
		return ""
	}
	return *v.Val
}

// Type gets the flag type
func (v *IPVar) Type() string {
	return "ip"
}

// IPPortVar is used for validating a command line option that represents an IP and a port. It implements the pflag.Value interface
type IPPortVar struct {
	Val *string
}

// Set sets the flag value
func (v *IPPortVar) Set(s string) error {
	if len(s) == 0 {
		v.Val = nil
		return nil
	}

	if v.Val == nil {
		// it's okay to panic here since this is programmer error
		panic("the string pointer passed into IPPortVar should not be nil")
	}

	// Both IP and IP:port are valid.
	// Attempt to parse into IP first.
	if netutils.ParseIPSloppy(s) != nil {
		*v.Val = s
		return nil
	}

	// Can not parse into IP, now assume IP:port.
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("%q is not in a valid format (ip or ip:port): %v", s, err)
	}
	if netutils.ParseIPSloppy(host) == nil {
		return fmt.Errorf("%q is not a valid IP address", host)
	}
	if _, err := netutils.ParsePort(port, true); err != nil {
		return fmt.Errorf("%q is not a valid number", port)
	}
	*v.Val = s
	return nil
}

// String returns the flag value
func (v *IPPortVar) String() string {
	if v.Val == nil {
		return ""
	}
	return *v.Val
}

// Type gets the flag type
func (v *IPPortVar) Type() string {
	return "ipport"
}

// PortRangeVar is used for validating a command line option that represents a port range. It implements the pflag.Value interface
type PortRangeVar struct {
	Val *string
}

// Set sets the flag value
func (v PortRangeVar) Set(s string) error {
	if _, err := utilnet.ParsePortRange(s); err != nil {
		return fmt.Errorf("%q is not a valid port range: %v", s, err)
	}
	if v.Val == nil {
		// it's okay to panic here since this is programmer error
		panic("the string pointer passed into PortRangeVar should not be nil")
	}
	*v.Val = s
	return nil
}

// String returns the flag value
func (v PortRangeVar) String() string {
	if v.Val == nil {
		return ""
	}
	return *v.Val
}

// Type gets the flag type
func (v PortRangeVar) Type() string {
	return "port-range"
}

// RegisterWithTaintsVar is used for validating a command line option that represents a register with taints. It implements the pflag.Value interface
type RegisterWithTaintsVar struct {
	Value *[]v1.Taint
}

// Set sets the flag value
func (t RegisterWithTaintsVar) Set(s string) error {
	if len(s) == 0 {
		*t.Value = nil
		return nil
	}
	sts := strings.Split(s, ",")
	corev1Taints, _, err := utiltaints.ParseTaints(sts)
	if err != nil {
		return err
	}
	var taints []v1.Taint
	for _, ct := range corev1Taints {
		taints = append(taints, v1.Taint{Key: ct.Key, Value: ct.Value, Effect: v1.TaintEffect(ct.Effect)})
	}
	*t.Value = taints
	return nil
}

// String returns the flag value
func (t RegisterWithTaintsVar) String() string {
	if len(*t.Value) == 0 {
		return ""
	}
	var taints []string
	for _, taint := range *t.Value {
		taints = append(taints, fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect))
	}
	return strings.Join(taints, ",")
}

// Type gets the flag type
func (t RegisterWithTaintsVar) Type() string {
	return "[]v1.Taint"
}
