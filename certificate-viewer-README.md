# SPIFFE Certificate Viewer

A simple web UI to view SPIFFE SVID certificates and CA bundle certificates for your workload.

## Overview

The SPIFFE Certificate Viewer consists of:

1. A React-based UI that displays X.509 certificates using the [@peculiar/certificates-viewer-react](https://www.npmjs.com/package/@peculiar/certificates-viewer-react) library
2. A Go HTTP server that:
   - Fetches certificates from the SPIFFE Workload API
   - Serves the UI as a single bundled asset
   - Injects the certificate data into the UI at render time

## Features

- Toggle between viewing SVID certificates and CA bundle certificates
- No client-side API calls - certificates are embedded during page render
- Designed to run as a sidecar container alongside your application

## Development

### Prerequisites

- Node.js and npm for the UI
- Go 1.21+ for the server

### UI Development

```bash
cd ui
npm install
npm run dev
```

The UI will run in development mode with placeholder certificate data.

### Server Development

```bash
cd server
go run main.go
```

The server will run on port 8080 by default.

### Building

Run the build script to build both the UI and server:

```bash
./build.sh
```

This will:
1. Build the UI with `npm run build`
2. Copy the UI build files to the server's `ui_dist` directory
3. Build the Go server binary

## Deployment

The certificate viewer is designed to run as a sidecar container, accessing the same SPIFFE Workload API socket as your main application.

Example Kubernetes configuration:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  containers:
  - name: main-app
    image: my-app:latest
    # ...
  - name: cert-viewer
    image: spiffe-cert-viewer:latest
    ports:
    - containerPort: 8080
    volumeMounts:
    - name: spiffe-workload-api
      mountPath: /run/spire/sockets
    env:
    - name: SPIFFE_ENDPOINT_SOCKET
      value: /run/spire/sockets/agent.sock
  volumes:
  - name: spiffe-workload-api
    hostPath:
      path: /run/spire/sockets
      type: DirectoryOrCreate
```

## License

[MIT](LICENSE)