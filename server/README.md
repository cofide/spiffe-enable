# SPIFFE Certificate Viewer Server

This Go server serves the SPIFFE Certificate Viewer UI and fetches certificates from the SPIFFE Workload API.

## Features

- Serves the certificate viewer UI as a single bundled asset
- Fetches SVID and CA Bundle certificates from the SPIFFE Workload API
- Injects certificate data into the UI at render time
- No client-side API calls needed - certificates are embedded in the page

## Development

```bash
# Run the server in development mode
go run main.go
```

The server will listen on port 8080 by default.

## Building

```bash
# Build just the server
go build -o spiffe-cert-viewer

# Or use the root build script to build the full stack
cd ..
./build.sh
```

## Configuring

The server can be configured with the following environment variables:

- `PORT`: The port to listen on (default: 8080)
- `SPIFFE_ENDPOINT_SOCKET`: The path to the SPIFFE Workload API socket (default: `/run/spire/sockets/agent.sock`)

## Production Deployment

For production use, the server should be deployed as a sidecar container alongside your application within the same Pod. This allows it to access the same SPIFFE Workload API socket.