#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

delete_instance_by_filter() {
  names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
  for name in $names
  do
    if [ $name == "NAME" ]; then
        continue
    fi

    if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]] || [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
        echo "delete instance name: $name"
        gcloud compute instances delete $name --project=${PROJECT} --zone=${ZONE} --quiet &
        sleep 1
    fi
  done
}


check_knownhosts_google(){
    if [[ -f "$HOME/.ssh/google_compute_known_hosts" || -d "$HOME/.ssh/google_compute_known_hosts" ]]; then
        echo -e "google known_hosts already exits, we remove this file first\n";
        rm -rf ~/.ssh/google_compute_known_hosts
    fi
}


delete_instance_by_filter

check_knownhosts_google

echo "all instance have been deleted seccessfully."