#!/bin/sh

# Builds the docker image and pushes it to docker hub.

set -x

docker build -t jheidel/jheidel-aprs .

docker push jheidel/jheidel-aprs:latest
