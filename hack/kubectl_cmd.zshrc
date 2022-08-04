kubectl_patch_app() {
 echo kubectl patch --kubeconfig kubeconfig --namespace $1 application $2 --patch-file $3
 kubectl patch --kubeconfig kubeconfig --namespace $1 application $2 --patch-file $3
}

kubectl_get_app() {
 echo kubectl get --kubeconfig kubeconfig --namespace $1 application $2 -o yaml
 kubectl get --kubeconfig kubeconfig --namespace $1 application $2 -o yaml
}
