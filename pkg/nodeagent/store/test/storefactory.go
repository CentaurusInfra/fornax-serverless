package main

import (
	"fmt"
	"os"
	"time"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/pod"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/session"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/factory"
	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/sqlite"
	_ "github.com/mattn/go-sqlite3"
	v1 "k8s.io/api/core/v1"
)

func main() {
	defer os.Remove("./nodeagent_test.db")
	start := time.Now().UnixMilli()

	var err error
	var podstore *factory.PodStore
	if podstore, err = factory.NewPodSqliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
		fmt.Println(err)
	}

	var sessionstore *factory.SessionStore
	if sessionstore, err = factory.NewSessionSqliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
		fmt.Println(err)
	}

	var containerstore *factory.ContainerStore
	if containerstore, err = factory.NewContainerSqliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
		fmt.Println(err)
	}

	count := 100
	for i := 0; i <= count; i++ {
		id := fmt.Sprint(i)
		podstore.PutPod(&pod.Pod{Identifier: id, Pod: v1.Pod{}, ConfigMap: v1.ConfigMap{}})
		podstore.GetPod(id)

		sessionstore.PutSession(&session.Session{Identifier: id})
		sessionstore.GetSession(id)

		containerstore.PutContainer(&pod.Container{Identifier: id})
		containerstore.GetContainer(id)
	}

	fmt.Println("get no 99 pod,session,container")
	fmt.Println(sessionstore.GetSession("99"))
	fmt.Println(containerstore.GetContainer("99"))
	fmt.Println(podstore.GetPod("99"))

	var list []interface{}
	list, _ = podstore.ListObject()
	for _, v := range list {
		f, ok := v.(*pod.Pod)
		fmt.Printf("Got pod no %s, correct? %v\n", f.Identifier, ok)
	}

	list, _ = sessionstore.ListObject()
	for _, v := range list {
		f, ok := v.(*session.Session)
		fmt.Printf("Got session no %s, correct? %v\n", f.Identifier, ok)
	}

	list, _ = containerstore.ListObject()
	for _, v := range list {
		f, ok := v.(*pod.Container)
		fmt.Printf("Got container no %s, correct? %v\n", f.Identifier, ok)
	}
	fmt.Println("delete all object")
	for i := 0; i <= count; i++ {
		podstore.DelObject(fmt.Sprint(i))
		containerstore.DelObject(fmt.Sprint(i))
		sessionstore.DelObject(fmt.Sprint(i))
	}

	fmt.Println(sessionstore.GetSession("99"))
	fmt.Println(containerstore.GetContainer("99"))
	fmt.Println(podstore.GetPod("99"))
	stop := time.Now().UnixMilli()
	fmt.Printf("%d milli seconds passed", stop-start)

}
