#!/bin/bash
set -e

# Build script for the SPIFFE Certificate Viewer
# This script builds the UI and embeds it into the Go server

echo "Building SPIFFE Certificate Viewer..."

# Step 1: Build the UI
echo "Building UI..."
cd ui
npm run build
cd ..

# Step 2: Copy the UI build to the server's ui_dist directory
echo "Copying UI build to server..."
rm -rf server/ui_dist
mkdir -p server/ui_dist
cp -r ui/dist/* server/ui_dist/

# Step 3: Build the Go server
echo "Building server..."
cd server
go build -o spiffe-cert-viewer
cd ..

echo "Build complete!"
echo "The server binary is at: server/spiffe-cert-viewer"
echo ""
echo "To run the server: ./server/spiffe-cert-viewer"