#!/usr/bin/env bash
# Creates every MiniStack resource. Idempotent: safe to run on each `docker compose up`.
set -euo pipefail
aws() { command aws --endpoint-url "$AWS_ENDPOINT_URL" --output text "$@"; }

queue() { # name -> creates name + name-dlq with redrive after 5 receives
  local dlq_url dlq_arn
  dlq_url=$(aws sqs create-queue --queue-name "$1-dlq" --query QueueUrl)
  dlq_arn=$(aws sqs get-queue-attributes --queue-url "$dlq_url" --attribute-names QueueArn --query Attributes.QueueArn)
  aws sqs create-queue --queue-name "$1" --attributes \
    "{\"VisibilityTimeout\":\"30\",\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"$dlq_arn\\\",\\\"maxReceiveCount\\\":\\\"5\\\"}\"}" >/dev/null
  echo "queue $1 (+dlq)"
}

table() { # name -> table with string partition key "pk"
  aws dynamodb describe-table --table-name "$1" >/dev/null 2>&1 || aws dynamodb create-table --table-name "$1" \
    --attribute-definitions AttributeName=pk,AttributeType=S --key-schema AttributeName=pk,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST >/dev/null
  echo "table $1"
}

# Messaging: commands go point-to-point on SQS; facts fan out via SNS -> SQS.
queue relay-requests
queue relay-results
queue media-relay-call-events
queue audit-vault-call-events
queue audio-chunks # media-relay -> audit-vault: encrypted call recordings

topic_arn=$(aws sns create-topic --name call-events --query TopicArn)
for q in media-relay-call-events audit-vault-call-events; do
  q_arn=$(aws sqs get-queue-attributes --queue-url "$(aws sqs get-queue-url --queue-name $q --query QueueUrl)" \
    --attribute-names QueueArn --query Attributes.QueueArn)
  aws sns subscribe --topic-arn "$topic_arn" --protocol sqs --notification-endpoint "$q_arn" \
    --attributes RawMessageDelivery=true >/dev/null
  echo "sns call-events -> $q"
done

# Database per service.
for t in signaling-calls signaling-users signaling-outbox signaling-processed signaling-accounts signaling-refresh \
         relay-sessions relay-nodes relay-outbox relay-processed audit-records audit-processed; do table "$t"; done

# Object storage + secret for audio encryption.
aws s3api head-bucket --bucket larpspeak-audio 2>/dev/null || aws s3api create-bucket --bucket larpspeak-audio >/dev/null
echo "bucket larpspeak-audio"
# Call recordings are encrypted twice: audit-vault seals every chunk with AES-256-GCM
# (key: larpspeak/audio-key) and the bucket encrypts whatever lands in it by default.
# The bucket is never public. Both calls are idempotent; an emulator that lacks one of
# these APIs only costs a warning (the application-level encryption does not depend on them).
aws s3api put-bucket-encryption --bucket larpspeak-audio --server-side-encryption-configuration \
  '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"},"BucketKeyEnabled":true}]}' >/dev/null \
  && echo "bucket larpspeak-audio: default encryption AES256" \
  || echo "WARN: could not enable default bucket encryption (SSE)"
aws s3api put-public-access-block --bucket larpspeak-audio --public-access-block-configuration \
  'BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true' >/dev/null \
  && echo "bucket larpspeak-audio: public access blocked" \
  || echo "WARN: could not block public access on the bucket"
aws secretsmanager describe-secret --secret-id larpspeak/audio-key >/dev/null 2>&1 || \
  aws secretsmanager create-secret --name larpspeak/audio-key --secret-string "$(head -c 32 /dev/urandom | base64)" >/dev/null
echo "secret larpspeak/audio-key"
# Ed25519 seed for JWTs: read only by call-signaling, the only service that creates tokens.
aws secretsmanager describe-secret --secret-id larpspeak/jwt-key >/dev/null 2>&1 || \
  aws secretsmanager create-secret --name larpspeak/jwt-key --secret-string "$(head -c 32 /dev/urandom | base64)" >/dev/null
echo "secret larpspeak/jwt-key"
echo "infra ready"
