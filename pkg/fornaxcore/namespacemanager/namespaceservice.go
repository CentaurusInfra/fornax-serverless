package namespacemanager

import (
	restful "github.com/emicklei/go-restful/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
)

func (nm *NamespaceManager) WebService() *restful.WebService {
	ws := new(restful.WebService)
	ws.Path("/namespaces").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)
	ws.Route(ws.POST("").To(nm.RestCreateNamespace))

	return ws
}

func (nm *NamespaceManager) RestCreateNamespace(request *restful.Request, response *restful.Response) {
	tenant := struct{ Name string }{}
	if err := request.ReadEntity(&tenant); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}

	if nm.GetNamespace(tenant.Name) != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusConflict, "tenant already exists")
		return
	}

	// todo: write ns into etcd, handle write error
	// for now, temporary to have ns record managed by ns-manager
	ns := Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenant.Name,
		},
	}
	err := nm.CreateNamespace(&ns)
	if err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}

	response.WriteHeaderAndEntity(http.StatusCreated, nm.namespaces[tenant.Name])
}
