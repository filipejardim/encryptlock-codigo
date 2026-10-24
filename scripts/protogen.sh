#!/usr/bin/env bash
# Regenerates the Go code for EncryptLock's protobuf messages (proto/encryptlock).
# Needs: go install github.com/bufbuild/buf/cmd/buf@v1.47.2
#        go install github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway@v1.16.0
#        go install github.com/cosmos/gogoproto/protoc-gen-gocosmos@v1.7.2
set -euo pipefail
cd "$(dirname "$0")/../proto"
rm -rf gen
buf generate --template buf.gen.yaml --path encryptlock
cp -r gen/encryptlock/lockkeeper/* ..
rm -rf gen
