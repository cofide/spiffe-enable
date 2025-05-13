# SPIFFE Certificate Viewer UI

A React-based UI for viewing SPIFFE SVID certificates and CA bundle certificates.

## Overview

This UI component displays X.509 certificates using the [@peculiar/certificates-viewer-react](https://www.npmjs.com/package/@peculiar/certificates-viewer-react) library. It provides a toggle to switch between viewing SVID certificates and CA bundle certificates.

## Development

```bash
npm install
npm run dev
```

The UI will run in development mode with placeholder certificate data.

## Building

```bash
npm run build
```

This will create a production build in the `dist` directory.

## Integration with Go Server

In production, this UI is embedded into a Go HTTP server that injects real certificate data from the SPIFFE Workload API. See the top-level README for more information about the complete system.