package applicationManager_test

import (
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/applicationManager"
	"github.com/emicklei/go-restful/v3"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateApplication(t *testing.T) {
	am := applicationManager.New()
	httpReq, _ := http.NewRequest("POST", "/namespaces/myns/applications", strings.NewReader(`{"metadata":{"name":"myapp"}}`))
	httpReq.Header = map[string][]string{"Content-Type": {restful.MIME_JSON}}
	req := restful.NewRequest(httpReq)
	req.PathParameters()["ns-name"] = "myns"

	recorder := httptest.NewRecorder()
	resp := restful.NewResponse(recorder)
	resp.SetRequestAccepts(restful.MIME_JSON)

	am.RestCreateApplication(req, resp)

	if resp.StatusCode() != http.StatusCreated {
		t.Fatalf("status code expected 201; got %d", resp.StatusCode())
	}

	httpResp := recorder.Result()
	defer httpResp.Body.Close()
	data, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("failed to get response body: %s", err)
	}
	t.Logf("response body: %s", data)
}
