#!/usr/bin/env bash

set -eo pipefail

export VERSION=$GITHUB_COMMIT-test
export KO_DOCKER_REPO=ko.local

set -u

(
  cd ../.. && \
  ko build --platform=all --base-import-paths --tags="latest" --image-user=1000 --image-annotation="" --image-label="" ./cmd/anubis -L
)

rm -rf ./var/repos ./var/foo
mkdir -p ./var/repos

(cd ./var/repos && git init --bare foo.git && cd foo.git && git config http.receivepack true)

docker compose up -d

sleep 2

(
  cd var && \
  mkdir foo && \
  cd foo && \
  git init && \
  touch README && \
  git add . && \
  git commit -sm "initial commit" && \
  git push http://localhost:3000/git/foo.git
)

docker compose down