package applicationManager

import (
	v1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	restful "github.com/emicklei/go-restful/v3"
	"net/http"
)

func (am *ApplicationManager) NewWebService() *restful.WebService {
	ws := new(restful.WebService)
	ws.Path("/namespaces/{ns-name}/applications")
	ws.Param(ws.PathParameter("ns-name", "name of namespace").DataType("string"))
	ws.Param(ws.PathParameter("app-name", "name of application").DataType("string"))
	ws.Consumes(restful.MIME_JSON)
	ws.Produces(restful.MIME_JSON)
	ws.Route(ws.POST("").To(am.RestCreateApplication))
	ws.Route(ws.DELETE("{app-name}").To(am.RestDeleteApplication))
	ws.Route(ws.PUT("{app-name}").To(am.RestUpdateApplication))
	return ws
}

func (am *ApplicationManager) RestCreateApplication(request *restful.Request, response *restful.Response) {
	nsName := request.PathParameter("ns-name")
	if len(nsName) == 0 {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, "namespace missing in url path")
		return
	}

	app := v1.Application{}
	if err := request.ReadEntity(&app); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}

	if len(app.Namespace) == 0 {
		app.Namespace = nsName
	} else if app.Namespace != nsName {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, "namespace specified not inline with url path")
		return
	}

	if am.GetApp(nsName, app.Name) != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusConflict, "application already exists")
		return
	}

	// todo: write app to etcd
	// for now, add it to app manager directly
	if err := am.CreateApp(nsName, &app); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusInternalServerError, err.Error())
	}

	response.WriteHeaderAndEntity(http.StatusCreated, app)
}

func (am *ApplicationManager) RestDeleteApplication(request *restful.Request, response *restful.Response) {
	nsName := request.PathParameter("ns-name")
	if len(nsName) == 0 {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, "namespace missing in url path")
		return
	}

	appName := request.PathParameter("app-name")
	if len(appName) == 0 {
		response.AddHeader("Content-Type", "text/plain")
		response.WriteErrorString(http.StatusBadRequest, "application name missing in url path")
		return
	}

	app := am.GetApp(nsName, appName)
	if app == nil {
		response.WriteHeaderAndEntity(http.StatusOK, nil)
		return
	}

	app.Status.State = v1.AppTerminating
	// todo: start termination workflow

	response.WriteHeaderAndEntity(http.StatusAccepted, app)
}

func (am *ApplicationManager) RestUpdateApplication(request *restful.Request, response *restful.Response) {
	// todo: impl
}
