#!/bin/sh

set -x

docker build -t jheidel/jheidel-aprs .

docker push jheidel/jheidel-aprs:latest
