package main

import (
  "fmt"
  "time"

  "centaurusinfra.io/fornax-serverless/pkg/nodeagent/store"
  "centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/factory"
  "centaurusinfra.io/fornax-serverless/pkg/nodeagent/store/sqlite"
  _ "github.com/mattn/go-sqlite3"
  v1 "k8s.io/api/core/v1"
)

func main() {
  start := time.Now().UnixMilli()

  var err error
  var podstore *factory.PodStore
  if podstore, err = factory.NewPodSqlliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
    fmt.Println(err)
  }

  var sessionstore *factory.SessionStore
  if sessionstore, err = factory.NewSessionSqlliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
    fmt.Println(err)
  }

  var containerstore *factory.ContainerStore
  if containerstore, err = factory.NewContainerSqlliteStore(&sqlite.SQLiteStoreOptions{ConnUrl: "./nodeagent_test.db"}); err != nil {
    fmt.Println(err)
  }

  count := 100
  for i := 0; i <= count; i++ {
    id := fmt.Sprint(i)
    podstore.PutPod(&store.Pod{Identifier: id, Pod: v1.Pod{}, ConfigMap: v1.ConfigMap{}})
    podstore.GetPod(id)

    sessionstore.PutSession(&store.Session{Identifier: id})
    sessionstore.GetSession(id)

    containerstore.PutContainer(&store.Container{Identifier: id})
    containerstore.GetContainer(id)
  }

  fmt.Println(sessionstore.GetSession(fmt.Sprint(99)))
  fmt.Println(containerstore.GetContainer(fmt.Sprint(99)))
  fmt.Println(podstore.GetPod(fmt.Sprint(99)))

  for i := 0; i <= count; i++ {
    podstore.DelObject(fmt.Sprint(i))
    containerstore.DelObject(fmt.Sprint(i))
    sessionstore.DelObject(fmt.Sprint(i))
  }
  fmt.Println(sessionstore.GetSession(fmt.Sprint(100)))
  fmt.Println(containerstore.GetContainer(fmt.Sprint(100)))
  fmt.Println(podstore.GetPod(fmt.Sprint(100)))
  stop := time.Now().UnixMilli()
  fmt.Printf("%d milli seconds passed", stop-start)

}
