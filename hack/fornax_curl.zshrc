fornax_post() {
 echo curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $3)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/$2
 curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $3)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/$2
}

fornax_get() {
 echo curl -H 'Content-Type: application/yaml' -X GET http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/$2/$3
 curl -H 'Content-Type: application/yaml' -X GET http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/$2/$3
}

fornax_application_get() {
 echo curl -H 'Content-Type: application/yaml' -X GET  http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications/$2
 curl -H 'Content-Type: application/yaml' -X GET  http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications/$2
}

fornax_application_update() {
 echo curl -H 'Content-Type: application/yaml' -X UPDATE -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications
 curl -H 'Content-Type: application/yaml' -X UPDATE -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications
}

fornax_application_patch() {
 echo curl -H 'Content-Type: application/yaml' -X PATCH -d "$(cat $3)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications/$2
 curl -H 'Content-Type: application/yaml' -X PATH -d "$(cat $3)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications/$2
}

fornax_application_post() {
 echo curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications
 curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications
}

kube_post() {
 echo curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $3)" http://127.0.0.1:8001/apis/k8s.io/v1/namespaces/$1/$2
 curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $3)" http://127.0.0.1:8001/apis/k8s.io/v1/namespaces/$1/$2
}

alias kubeproxy='kubectl --kubeconfig kubeconfig proxy --address localhost'
