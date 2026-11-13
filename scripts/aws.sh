#!/usr/bin/env bash
# AWS CLI against the running MiniStack (no local AWS CLI needed). Usage: scripts/aws.sh sqs list-queues
exec docker run --rm -i --network larpspeak_default \
  -e AWS_ACCESS_KEY_ID=test -e AWS_SECRET_ACCESS_KEY=test -e AWS_REGION=us-east-1 \
  amazon/aws-cli:2.39.0 --endpoint-url http://ministack:4566 --output text "$@"
