#! /bin/bash

set -e


FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

stop_instance_by_filter() {
  names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
  for name in $names
  do
    if [ $name == "NAME" ]; then
        continue
    fi

    if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]] || [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
        echo "delete instance name: $name"
        gcloud compute instances stop $name --async --project=${PROJECT} --zone=${ZONE} --quiet &
        sleep 1
    fi
  done
}

stop_instance_by_filter

echo "all instance have been stopped seccessfully."