package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/spiffe/go-spiffe/v2/logger"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

const (
	apiTimeout   = 5 * time.Second
	spiffeSocket = "unix:///spiffe-workload-api/spire-agent.sock"
	timeFormat   = time.RFC3339
)

type Certificate struct {
	Name        string `json:"name"`
	Certificate string `json:"certificate"`
}

type PageData struct {
	SVIDCertificates template.JS
	CACertificates   template.JS
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	client, err := workloadapi.New(ctx, workloadapi.WithAddr(spiffeSocket), workloadapi.WithLogger(logger.Std))
	if err != nil {
		log.Fatalf("Unable to create workload API client: %v", err)
	}

	defer client.Close()

	// Load the dashboard template
	tmpl, err := template.ParseFiles("templates/dashboard.tmpl")
	if err != nil {
		log.Fatalf("Error parsing template: %v", err)
	}

	// Serve static files
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Serve the dashboard
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Get SVID certificates
		svidCerts, err := loadSVIDCertificates(ctx, client)
		if err != nil {
			log.Printf("Error loading SVID certificates: %v", err)
			http.Error(w, "Error loading certificates", http.StatusInternalServerError)
			return
		}

		caCerts, err := loadCACertificates(ctx, client)
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
			SVIDCertificates: template.JS(svidCertsJSON),
			CACertificates:   template.JS(caCertsJSON),
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
	var certificates []Certificate

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
			Certificate: base64.StdEncoding.EncodeToString(cert),
		}
		certificates = append(certificates, c)
	}

	return certificates, nil
}

func loadCACertificates(ctx context.Context, client *workloadapi.Client) ([]Certificate, error) {
	var certificates []Certificate

	bundles, err := client.FetchX509Bundles(ctx)
	if bundles == nil {
		return nil, fmt.Errorf("no trust bundles available")
	}

	if err != nil {
		slog.Warn("unable to fetch X.509 trust bundles", "error", err)
	}

	for _, b := range bundles.Bundles() {
		for _, c := range b.X509Authorities() {
			cert := Certificate{
				Name:        b.TrustDomain().IDString(),
				Certificate: base64.StdEncoding.EncodeToString(c.Raw),
			}
			certificates = append(certificates, cert)
		}
	}

	return certificates, nil
}
