package tenantManager_test

import (
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/tenantManager"
	"github.com/emicklei/go-restful/v3"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTenant(t *testing.T) {
	tm := tenantManager.New()
	httpReq, _ := http.NewRequest("POST", "/tenants", strings.NewReader(`{"name":"foo"}`))
	httpReq.Header = map[string][]string{
		"Content-Type": {restful.MIME_JSON},
	}
	req := restful.NewRequest(httpReq)

	recorder := httptest.NewRecorder()
	resp := restful.NewResponse(recorder)
	resp.SetRequestAccepts(restful.MIME_JSON)

	tm.CreateTenant(req, resp)

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
