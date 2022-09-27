curl_get_app() {
 echo curl -H 'Content-Type: application/yaml' -X GET  http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications/$2
 curl -H 'Content-Type: application/yaml' -X GET  http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications/$2
}

post_app() {
 echo curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications
 curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applications
}

post_session() {
 echo curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applicationsessions
 curl -H 'Content-Type: application/yaml' -X POST -d "$(cat $2)" http://127.0.0.1:8001/apis/core.fornax-serverless.centaurusinfra.io/v1/namespaces/$1/applicationsessions
}

patch_app() {
 kubectl patch --kubeconfig kubeconfig --namespace $1 application $2 --patch-file $3
}

patch_session() {
 kubectl patch --kubeconfig kubeconfig --namespace $1 applicationsession $2 --patch-file $3
}

del_app() {
 kubectl delete application --kubeconfig kubeconfig --namespace $1 $2
}

del_session() {
 kubectl delete applicationsession --kubeconfig kubeconfig --namespace $1 $2
}

get_apps() {
 kubectl get application --kubeconfig kubeconfig -o yaml --namespace $*
}

get_sessions() {
 kubectl get applicationsession --kubeconfig kubeconfig -o yaml --namespace $*
}


alias kubeproxy='kubectl --kubeconfig kubeconfig proxy --address localhost'
alias kubectl='kubectl --kubeconfig kubeconfig'
