package applicationManager

import (
	v1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"fmt"
	"sync"
)

type ApplicationManager struct {
	sync.RWMutex
	apps map[string]v1.Application // index by ns+app-name
}

func New() *ApplicationManager {
	return &ApplicationManager{
		apps: make(map[string]v1.Application),
	}
}

func genKey(ns, app string) string {
	return fmt.Sprintf("%s/%s", ns, app)
}

func (am *ApplicationManager) GetApp(ns, name string) *v1.Application {
	key := genKey(ns, name)

	am.RLock()
	defer am.RUnlock()

	app, ok := am.apps[key]
	if !ok {
		return nil
	}

	return &app
}

func (am *ApplicationManager) CreateApp(ns string, app *v1.Application) error {
	if len(ns) == 0 {
		return fmt.Errorf("namespace name should not be empty")
	}

	if app == nil {
		return fmt.Errorf("invalid null application")
	}

	if len(app.Name) == 0 {
		return fmt.Errorf("application name should not be empty")
	}

	key := genKey(ns, app.Name)
	am.Lock()
	defer am.Unlock()

	if _, ok := am.apps[key]; ok {
		return fmt.Errorf("named resource already exists")
	}

	am.apps[key] = *app
	return nil
}

// todo: list/watch app from etcd
// todo: init app pod pool
