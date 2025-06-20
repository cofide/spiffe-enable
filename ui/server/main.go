package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/spiffe/go-spiffe/v2/logger"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

const (
	apiTimeout          = 30 * time.Second
	defaultSpiffeSocket = "unix:///spiffe-workload-api/spire-agent.sock"
)

var (
	spiffeSocket string
)

//go:embed static
var uiAssets embed.FS

//go:embed templates
var tmplAssets embed.FS

type Certificate struct {
	Name        string `json:"name"`
	TrustDomain string `json:"td"`
	Certificate string `json:"certificate"`
}

type PageData struct {
	SpiffeID              string
	TrustDomain           string
	FederatedTrustDomains []string
	SVIDCertificates      template.JS
	CACertificates        template.JS
}

func init() {
	if socketStr := os.Getenv("SPIFFE_ENDPOINT_SOCKET"); socketStr != "" {
		spiffeSocket = socketStr
	} else {
		spiffeSocket = defaultSpiffeSocket
	}

	log.Printf("Using SPIFFE endpoint socket: %s\n", spiffeSocket)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	client, err := workloadapi.New(ctx, workloadapi.WithAddr(spiffeSocket), workloadapi.WithLogger(logger.Std))
	if err != nil {
		log.Fatalf("Unable to create workload API client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("Error closing workload API client: %v", err)
		}
	}()

	subTmplFS, err := fs.Sub(tmplAssets, "templates")
	if err != nil {
		log.Fatalf("Failed to create sub-filesystem: %v", err)
	}

	// Load the dashboard template
	tmplFile, err := subTmplFS.Open("dashboard.tmpl")
	if err != nil {
		log.Fatalf("Failed to open template file: %v", err)
	}
	defer func() {
		if err := tmplFile.Close(); err != nil {
			log.Printf("Error closing template file: %v", err)
		}
	}()

	// Read the content of the template file
	tmplBytes, err := io.ReadAll(tmplFile)
	if err != nil {
		log.Fatalf("Failed to read template file: %v", err)
	}

	// Create a new template and parse its content
	tmpl, err := template.New("dashboard").Parse(string(tmplBytes))
	if err != nil {
		log.Fatalf("Error parsing template: %v", err)
	}

	// Create a sub-filesystem for the embedded UI assets
	subFS, err := fs.Sub(uiAssets, "static")
	if err != nil {
		log.Fatalf("Failed to create sub-filesystem: %v", err)
	}

	// Set up a file server for static assets
	fileServer := http.FileServer(http.FS(subFS))

	// Serve static files
	http.Handle("/static/", http.StripPrefix("/static/", fileServer))

	// Serve the dashboard
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCtx, reqCancel := context.WithTimeout(r.Context(), apiTimeout)
		defer reqCancel()

		// Get SVID certificates
		svidCerts, err := loadSVIDCertificates(reqCtx, client)
		if err != nil {
			log.Printf("Error loading SVID certificates: %v", err)
			http.Error(w, "Error loading certificates", http.StatusInternalServerError)
			return
		}

		caCerts, federatedTDs, err := loadCACertificates(reqCtx, client, svidCerts[0].TrustDomain)
		if err != nil {
			log.Printf("Error loading CA certificates: %v", err)
			http.Error(w, "Error loading certificates", http.StatusInternalServerError)
			return
		}

		svidCertsJSON, err := json.Marshal(svidCerts)
		if err != nil {
			log.Printf("Error marshaling SVID certificates: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		caCertsJSON, err := json.Marshal(caCerts)
		if err != nil {
			log.Printf("Error marshaling CA certificates: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Prepare data for template
		data := PageData{
			SpiffeID:              svidCerts[0].Name,
			TrustDomain:           svidCerts[0].TrustDomain,
			FederatedTrustDomains: federatedTDs,
			SVIDCertificates:      template.JS(svidCertsJSON),
			CACertificates:        template.JS(caCertsJSON),
		}

		// Execute template with data
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Error executing template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	})

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func loadSVIDCertificates(ctx context.Context, client *workloadapi.Client) ([]Certificate, error) {
	certificates := []Certificate{}

	svids, err := client.FetchX509SVIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch X.509 SVIDs: %s", err)
	}

	for _, s := range svids {
		cert, _, err := s.MarshalRaw()
		if err != nil {
			return nil, fmt.Errorf("unable to marshal X.509 SVID: %s", err)
		}

		c := Certificate{
			Name:        s.ID.URL().String(),
			TrustDomain: s.ID.TrustDomain().Name(),
			Certificate: base64.StdEncoding.EncodeToString(cert),
		}
		certificates = append(certificates, c)
	}

	return certificates, nil
}

func loadCACertificates(
	ctx context.Context, client *workloadapi.Client, ownTrustDomainID string,
) ([]Certificate, []string, error) {
	var certificates []Certificate
	var uniqueTrustDomainIDs []string

	bundles, err := client.FetchX509Bundles(ctx)
	if bundles == nil {
		return nil, nil, fmt.Errorf("no trust bundles available")
	}

	if err != nil {
		slog.Warn("unable to fetch X.509 trust bundles", "error", err)
	}

	seenTrustDomainIDs := make(map[string]struct{})
	seenTrustDomainIDs[ownTrustDomainID] = struct{}{}

	for _, b := range bundles.Bundles() {
		trustDomainID := b.TrustDomain().Name()

		if _, found := seenTrustDomainIDs[trustDomainID]; !found {
			uniqueTrustDomainIDs = append(uniqueTrustDomainIDs, trustDomainID)
			seenTrustDomainIDs[trustDomainID] = struct{}{}
		}

		for _, c := range b.X509Authorities() {
			cert := Certificate{
				Name:        trustDomainID,
				Certificate: base64.StdEncoding.EncodeToString(c.Raw),
			}
			certificates = append(certificates, cert)
		}
	}

	return certificates, uniqueTrustDomainIDs, nil
}
