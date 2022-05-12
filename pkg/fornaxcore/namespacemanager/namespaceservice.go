package namespacemanager

import (
	restful "github.com/emicklei/go-restful/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
)

func (tm NamespaceManager) WebService() *restful.WebService {
	ws := new(restful.WebService)
	ws.Path("/namespaces").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)
	ws.Route(ws.POST("").To(tm.CreateTenant))

	return ws
}

func (tm NamespaceManager) CreateTenant(request *restful.Request, response *restful.Response) {
	tenant := struct{ Name string }{}
	if err := request.ReadEntity(&tenant); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, err.Error())
	}

	if _, ok := tm.tenants[tenant.Name]; ok {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusConflict, "tenant already exists")
	}

	tm.tenants[tenant.Name] = Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenant.Name,
		},
	}

	response.WriteHeaderAndEntity(http.StatusCreated, tm.tenants[tenant.Name])
}
