#!/bin/bash

set -e

echo "🔨 Generating gRPC code from proto files..."

# Diretório de saída
OUTPUT_DIR="api/proto"

# Compila o proto file
protoc \
  --go_out=$OUTPUT_DIR \
  --go_opt=paths=source_relative \
  --go-grpc_out=$OUTPUT_DIR \
  --go-grpc_opt=paths=source_relative \
  api/proto/transcription.proto

echo "✅ Proto files generated successfully!"
echo "   - $OUTPUT_DIR/transcription.pb.go"
echo "   - $OUTPUT_DIR/transcription_grpc.pb.go"