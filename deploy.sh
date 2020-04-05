#!/bin/sh

set -x

docker pull jheidel/jheidel-aprs

docker stop jheidel-aprs
docker rm jheidel-aprs

docker run \
        -d --restart always \
        --name jheidel-aprs \
        --mount type=bind,source=/etc/jheidel-aprs,target=/etc/jheidel-aprs \
        --env "DOCKER_HOST=$( hostname )" \
        jheidel/jheidel-aprs

sleep 3

docker ps --filter="name=jheidel-aprs"
docker logs jheidel-aprs
