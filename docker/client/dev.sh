#!/bin/sh
# Generate the client's protobuf types, keep them current as the proto tree
# changes, and run the Next.js dev server. Runs from the repository root so the
# buf template's paths resolve.
set -e
cd /app

gen="client/node_modules/.bin/buf generate --template buf.gen.ts.yaml --include-imports"
$gen
client/node_modules/.bin/chokidar 'proto/**/*.proto' buf.gen.ts.yaml -c "$gen" &

cd client && exec npm run dev
