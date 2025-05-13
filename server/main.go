package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
)

// Embed the built UI assets (this will be used in the production build)
//go:embed ui_dist
var uiAssets embed.FS

// Certificate data to inject into the UI
type CertificateData struct {
	SVIDCertificates      []string `json:"svidCertificates"`
	CABundleCertificates  []string `json:"caBundleCertificates"`
}

// FetchSPIFFECertificates fetches certificates from the SPIFFE Workload API
// This is a placeholder that you can fill out with the actual implementation
func FetchSPIFFECertificates() (CertificateData, error) {
	// TODO: Implement fetching from SPIFFE Workload API
	// 1. Connect to the Workload API Unix socket
	// 2. Fetch SVID and trust bundle certificates
	// 3. Format as PEM strings
	
	// For now, returning placeholder certificates
	log.Println("Fetching certificates from SPIFFE Workload API (placeholder implementation)")
	
	return CertificateData{
		// Example data - will be replaced with actual certs
		SVIDCertificates: []string{
			`-----BEGIN CERTIFICATE-----
MIIDXTCCAkWgAwIBAgIJANMPt4IFy8otMA0GCSqGSIb3DQEBCwUAMHExCzAJBgNV
BAYTAkdCMQ8wDQYDVQQIDAZFbmdsYW5kMQ0wCwYDVQQHDARCYXRoMRkwFwYDVQQK
DBBTYW1wbGUgT3JnYW5pemUxCjAIBgNVBAsMAVUxFTATBgNVBAMMDHNhbXBsZS5j
b20wHhcNMjQwMTAxMDAwMDAwWhcNMzQwMTMwMDAwMDAwWjBxMQswCQYDVQQGEwJH
QjEPMA0GCSqGSIb3DQEBCwUAMHExCzAJBgNVBAYTAkdCMQ8wDQYDVQQIDAZFbmds
YW5kMQ0wCwYDVQQHDARCYXRoMRkwFwYDVQQKDBBTYW1wbGUgT3JnYW5pemUxCjAI
BgNVBAsMAVUxFTATBgNVBAMMDHNhbXBsZS5jb20wggEiMA0GCSqGSIb3DQEBAQUA
A4IBDwAwggEKAoIBAQDaL+2sLg9NclRk32o8vDhdX1EJD0ZtT0Iiy3mtF1JS6H3Q
g7xveRkXgXoQvTdXO0vLqW73zKqC6J1R2m9P3Xf5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5AgMBAAGjUzBRMB0GA1Ud
DgQWBBTBV8Id9y7r0HhYj5+xGPxW2p9HbjAfBgNVHSMEGDAWgBTBV8Id9y7r0HhY
j5+xGPxW2p9HbjAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQBC
5rF4N0K5L1R2m9P3Xf5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5XfO5
-----END CERTIFICATE-----`,
		},
		CABundleCertificates: []string{
			`-----BEGIN CERTIFICATE-----
MIIEQzCCAyugAwIBAgIUF7w7GvsQlzq6HwJ+wOheJZw+GWwwDQYJKoZIhvcNAQEL
BQAwgbQxCzAJBgNVBAYTAlVTMRMwEQYDVQQIEwpDYWxpZm9ybmlhMRYwFAYDVQQH
Ew1TYW4gRnJhbmNpc2NvMRswGQYDVQQKExJDbG91ZGZsYXJlLCBJbmMuMR8wHQYD
VQQLExZDbG91ZGZsYXJlIEVuZ2luZWVyaW5nMSMwIQYDVQQDExp0cnVzdC1jYS1j
ZXJ0aWZpY2F0ZS5sb2NhbDEbMBkGCSqGSIb3DQEJARYMdGVzdEBzcGlmZmUwHhcN
MjMwMTEwMDAwMDAwWhcNMzMwMTEwMDAwMDAwWjCBtDELMAkGA1UEBhMCVVMxEzAR
BgNVBAgTCkNhbGlmb3JuaWExFjAUBgNVBAcTDVNhbiBGcmFuY2lzY28xGzAZBgNV
BAoTEkNsb3VkZmxhcmUsIEluYy4xHzAdBgNVBAsTFkNsb3VkZmxhcmUgRW5naW5l
ZXJpbmcxIzAhBgNVBAMTGnRydXN0LWNhLWNlcnRpZmljYXRlLmxvY2FsMRswGQYJ
KoZIhvcNAQkBFgx0ZXN0QHNwaWZmZTABAgMEBQYHCAkKCwwNDg8IAwECAQIDAgMD
AwQDAwQEBAUEBQUFBgUGBgYHBgcHAgMBAgECAwIDAwQDAwQEBAUEBQUFBgUGBgYH
BgcHAgMBAgECAwIDAwQDAwQEBAUEBQUFBgUGBgYHBgcHAgMBAgECAwIDAwQDAwQE
BAUEBQUFBgUGBgYHBgcHBQMBAQEDAgMEBAUGBwgJCgsMDQ4PAgMBAQIDAgMDBAMD
BAQEBQQFBQUGBQYGBgcGBwcCAwEBAQMCAwQEBQYHCAkKCwwNDg8CAwEBAQMCAwQE
BQYHCAkKCwwNDg8CAwEBAQMCAwQEBQYHCAkKCwwNDg8CAwEBAQMCAwQEBQYHCAkK
CwwNDg8AAAAAAAAAAAAAMBYGCCsGAQUFBwEBBggrBgEFBQcBAQEBADAMBgNVHRMB
Af8EAjAAMA0GCSqGSIb3DQEBCwUAA4IBAQBOQCMVkw7VZYjRxygXb1gUBSwKgLOY
oJQGXtU5Jt5ek1gP06/Q92GJhP5LSPWNZpuhUW5JlKPdM3QSZRwA1MVXoGnmdnGw
8g==
-----END CERTIFICATE-----`,
		},
	}, nil
}

func main() {
	// Configure the HTTP server
	port := 8080
	
	// Override port from environment variable if provided
	if envPort := os.Getenv("PORT"); envPort != "" {
		if _, err := fmt.Sscanf(envPort, "%d", &port); err != nil {
			log.Printf("Warning: Invalid PORT environment variable: %s, using default: %d", envPort, port)
		}
	}
	
	// Create HTTP server routes
	mux := http.NewServeMux()
	
	// Serve the index page with injected certificate data
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		
		// Fetch certificates from SPIFFE Workload API
		certData, err := FetchSPIFFECertificates()
		if err != nil {
			log.Printf("Error fetching certificates: %v", err)
			http.Error(w, "Error fetching certificates", http.StatusInternalServerError)
			return
		}
		
		// Convert cert data to JSON to inject into the template
		svidCertsJSON, err := json.Marshal(certData.SVIDCertificates)
		if err != nil {
			http.Error(w, "Error preparing SVID certificate data", http.StatusInternalServerError)
			return
		}
		
		// Convert CA bundle certificates to JSON
		caBundleCertsJSON, err := json.Marshal(certData.CABundleCertificates)
		if err != nil {
			http.Error(w, "Error preparing CA bundle certificate data", http.StatusInternalServerError)
			return
		}
		
		// Read the index.html template
		// In production, this will use the embedded file system
		// For now, we'll use a hardcoded template for development
		indexHTML := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>SPIFFE Certificate Viewer</title>
  {{ range .CSSFiles }}
  <link rel="stylesheet" href="{{ . }}">
  {{ end }}
</head>
<body>
  <div id="root"></div>
  
  <script>
    // Inject certificate data into the page
    window.SPIFFE_SVID_CERTIFICATES = {{ .SVIDCertificatesJSON }};
    window.SPIFFE_CA_BUNDLE_CERTIFICATES = {{ .CABundleCertificatesJSON }};
  </script>
  
  {{ range .JSFiles }}
  <script type="module" src="{{ . }}"></script>
  {{ end }}
</body>
</html>`
		
		// Parse the template
		tmpl, err := template.New("index").Parse(indexHTML)
		if err != nil {
			http.Error(w, "Error parsing template", http.StatusInternalServerError)
			return
		}
		
		// Prepare data for the template
		// For production, these would be the actual asset paths from the build
		tmplData := struct {
			CSSFiles                []string
			JSFiles                 []string
			SVIDCertificatesJSON    template.JS
			CABundleCertificatesJSON template.JS
		}{
			CSSFiles:                []string{"/assets/index.css"},
			JSFiles:                 []string{"/assets/index.js"},
			SVIDCertificatesJSON:    template.JS(svidCertsJSON),
			CABundleCertificatesJSON: template.JS(caBundleCertsJSON),
		}
		
		// Execute the template
		err = tmpl.Execute(w, tmplData)
		if err != nil {
			http.Error(w, "Error executing template", http.StatusInternalServerError)
			return
		}
	})
	
	// Create a sub-filesystem for the embedded UI assets
	subFS, err := fs.Sub(uiAssets, "ui_dist")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}
	
	// Set up a file server for static assets
	fileServer := http.FileServer(http.FS(subFS))
	
	// Serve static assets from the embedded filesystem
	// This handles assets like CSS, JS, images, etc.
	mux.Handle("/assets/", http.StripPrefix("/assets/", fileServer))
	
	// For development or if embedded assets aren't available:
	if _, err := fs.Stat(subFS, "index.html"); err != nil {
		log.Println("Warning: UI assets not found in embedded filesystem. Using development placeholders.")
		
		// Fallback for development - serve placeholder content
		mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/assets/")
			
			if strings.HasSuffix(path, ".css") {
				w.Header().Set("Content-Type", "text/css")
				fmt.Fprintf(w, "/* CSS file %s */", path)
			} else if strings.HasSuffix(path, ".js") {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprintf(w, "// JS file %s", path)
			} else {
				http.NotFound(w, r)
			}
		})
	}
	
	// Start the server
	log.Printf("Starting SPIFFE Certificate Viewer server on :%d", port)
	
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

// TO BE IMPLEMENTED: Certificate fetcher for the SPIFFE Workload API
// func NewCertificateFetcher() *CertificateFetcher {
//     return &CertificateFetcher{}
// }
//
// type CertificateFetcher struct {
//     // Configuration and state for the fetcher
// }
//
// func (cf *CertificateFetcher) FetchCertificates() (CertificateData, error) {
//     // Implement fetching from SPIFFE Workload API
//     return CertificateData{}, nil
// }