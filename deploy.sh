#!/bin/bash
# Local to Remote Deployment Orchestrator for Udin's Canvas

SERVER="inikah-my-admin@103.93.163.44"
APP_ROOT="/var/www/udin-canvas"
TIMESTAMP=$(date +%Y%m%d%H%M%S)
RELEASE_DIR="$APP_ROOT/releases/$TIMESTAMP"

echo "-> 1. Compiling CSS and Go binary locally..."
make css
make templ
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /tmp/udin-canvas .

echo "-> 2. Pushing the monolithic binary to the bare-metal server..."
ssh "$SERVER" "mkdir -p $RELEASE_DIR"

rsync -avz --progress /tmp/udin-canvas "$SERVER:$RELEASE_DIR/udin-canvas"

# Firebase service account
rsync -avz --progress canvas-78802-firebase-adminsdk-fbsvc-90b0436346.json "$SERVER:$RELEASE_DIR/"

# Server-side Makefile
rsync -avz --progress Makefile.server "$SERVER:$RELEASE_DIR/Makefile"

echo "-> 3. Triggering atomic shift and systemd restart..."
ssh "$SERVER" "cd $RELEASE_DIR && make -f Makefile deploy"

echo "-> 4. Cleaning up local temp binary..."
rm -f /tmp/udin-canvas

echo "Deployment complete."
