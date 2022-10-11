#! /bin/bash

set -e

# delete virtual machine instance
gcloud compute instances delete davidzhu-fornaxcore --zone=us-central1-a
gcloud compute instances delete davidzhu-instance-1 --zone=us-central1-a
gcloud compute instances delete davidzhu-instance-2 --zone=us-central1-a