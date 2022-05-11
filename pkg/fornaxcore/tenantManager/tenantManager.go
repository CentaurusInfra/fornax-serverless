package tenantManager

import corev1 "k8s.io/api/core/v1"

type Tenant = corev1.Namespace

type TenantManager struct {
	tenants map[string]Tenant
}

func New() *TenantManager {
	return &TenantManager{
		tenants: make(map[string]Tenant),
	}
}
