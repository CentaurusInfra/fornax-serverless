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
package network

import (
	"errors"
	"net"

	v1 "k8s.io/api/core/v1"
)

type NetworkAddressProvider interface {
	GetNetAddress() ([]v1.NodeAddress, error)
}

var _ NetworkAddressProvider = &LocalNetworkAddressProvider{}

type LocalNetworkAddressProvider struct {
	NodeIPs  []net.IP
	Hostname string
}

func GetLocalV4IP() ([]net.IP, error) {
	ips := []net.IP{}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []net.IP{}, err
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if v := ipnet.IP.To4(); v != nil {
				// only get normal ip
				if !(v.IsLoopback() || v.IsLinkLocalMulticast() || v.IsLinkLocalUnicast() || v.IsUnspecified() || v.IsMulticast() || v.IsInterfaceLocalMulticast()) {
					ips = append(ips, v)
				}
			}
		}
	}
	return ips, nil
}

func (p *LocalNetworkAddressProvider) GetNetAddress() ([]v1.NodeAddress, error) {
	var err error
	var nodeIP, secondaryNodeIP, externalNodeIP net.IP
	if len(p.NodeIPs) == 0 {
		p.NodeIPs, err = GetLocalV4IP()
		if err != nil {
			return nil, err
		}
	}

	for _, v := range p.NodeIPs {
		if v.IsPrivate() && !v.IsUnspecified() {
			// use private ip as primary node ip
			if nodeIP == nil {
				nodeIP = v
			} else {
				if secondaryNodeIP == nil {
					secondaryNodeIP = v
				}
			}
		} else {
			if externalNodeIP == nil {
				externalNodeIP = v
			}
		}
	}

	if nodeIP == nil {
		return nil, errors.New("host does not have a valid private ipv4 address")
	}

	addresses := []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: nodeIP.String()},
	}
	if secondaryNodeIP != nil {
		addresses = append(addresses, v1.NodeAddress{Type: v1.NodeInternalIP, Address: secondaryNodeIP.String()})
	}
	if externalNodeIP != nil {
		addresses = append(addresses, v1.NodeAddress{Type: v1.NodeExternalIP, Address: externalNodeIP.String()})
	}
	addresses = append(addresses, v1.NodeAddress{Type: v1.NodeHostName, Address: p.Hostname})

	return addresses, nil
}
