#!/usr/bin/env bash
set -euo pipefail

# build image with ko
export KO_DOCKER_REPO=gcr.io/ahmet-personal-api

img="$(ko build .)"

. .envrc

# deploy a job to cloud run
set -x
gcloud beta run jobs update "home-ac-stats" \
	--project=ahmet-personal-api \
	--region=us-central1 \
	--image="$img" \
	--max-retries=0 \
	--task-timeout=15 \
	--set-env-vars="SENSIBO_API_KEY=$SENSIBO_API_KEY"
