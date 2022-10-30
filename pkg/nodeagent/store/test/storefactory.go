package main

import (
	"fmt"
	"os"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store"
	fornaxtypes "centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	"centaurusinfra.io/fornax-serverless/pkg/store/storage/sqlite"
	_ "github.com/mattn/go-sqlite3"
	v1 "k8s.io/api/core/v1"
)

func main() {
	defer os.Remove("./nodeagent_test.db")
	start := time.Now().UnixMilli()

	var err error
	var podstore *store.PodStore
	if podstore, err = store.NewPodSqliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
		fmt.Println(err)
	}

	var sessionstore *store.SessionStore
	if sessionstore, err = store.NewSessionSqliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
		fmt.Println(err)
	}

	count := 100
	for i := 0; i <= count; i++ {
		id := fmt.Sprint(i)
		podstore.PutPod(&fornaxtypes.FornaxPod{Identifier: id, Pod: &v1.Pod{}, ConfigMap: &v1.ConfigMap{}}, int64(i))
		podstore.GetPod(id)

		sessionstore.PutSession(&fornaxtypes.FornaxSession{Identifier: id}, int64(i))
		sessionstore.GetSession(id)

		fmt.Println("get no 99 pod,session")
		fmt.Println(sessionstore.GetSession("99"))
		fmt.Println(podstore.GetPod("99"))

		var list []interface{}
		list, _ = podstore.ListObject()
		for _, v := range list {
			f, ok := v.(*fornaxtypes.FornaxPod)
			fmt.Printf("Got pod no %s, correct? %v\n", f.Identifier, ok)
		}

		list, _ = sessionstore.ListObject()
		for _, v := range list {
			f, ok := v.(*fornaxtypes.FornaxSession)
			fmt.Printf("Got session no %s, correct? %v\n", f.Identifier, ok)
		}

		fmt.Println("delete all object")
		for i := 0; i <= count; i++ {
			podstore.DelObject(fmt.Sprint(i))
			sessionstore.DelObject(fmt.Sprint(i))
		}

		fmt.Println(sessionstore.GetSession("99"))
		fmt.Println(podstore.GetPod("99"))
		stop := time.Now().UnixMilli()
		fmt.Printf("%d milli seconds passed", stop-start)

	}
}
