// src/CertificateDisplay.js
import React, { useState } from 'react';
import { PeculiarCertificatesViewer } from '@peculiar/certificates-viewer-react';

// Default sample data (used only if no props are provided)
const defaultSvidCertificates = [
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
-----END CERTIFICATE-----`.trim(),
];

const defaultCaBundleCertificates = [
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
-----END CERTIFICATE-----`.trim(),
];

/**
 * CertDisplayTable component for displaying SPIFFE certificates
 * 
 * @param {Object} props - Component props
 * @param {Array<string>} [props.svidCertificates] - Array of PEM-encoded SVID certificates
 * @param {Array<string>} [props.caBundleCertificates] - Array of PEM-encoded CA bundle certificates
 * @param {Function} [props.onSelectCertificate] - Callback when a certificate is selected
 */
function CertDisplayTable({ 
  svidCertificates = defaultSvidCertificates,
  caBundleCertificates = defaultCaBundleCertificates,
  onSelectCertificate
}) {
  // State to track which set of certificates to display
  const [viewMode, setViewMode] = useState('svid');
  
  // Certificate data based on current view mode
  const certificates = viewMode === 'svid' ? svidCertificates : caBundleCertificates;
  
  // Title based on current view mode
  const title = viewMode === 'svid' ? 'SVID Certificates' : 'CA Bundle Certificates';

  // Optional: Define how to handle row clicks, if needed
  const handleRowClick = (event) => {
    if (event.detail && event.detail.certificate) {
      console.log('Clicked certificate:', event.detail.certificate);
      if (onSelectCertificate) {
        onSelectCertificate(event.detail.certificate);
      }
    } else if (event.detail && typeof event.detail.index === 'number') {
      console.log('Clicked certificate at index:', event.detail.index, certificates[event.detail.index]);
      if (onSelectCertificate) {
        onSelectCertificate(certificates[event.detail.index]);
      }
    }
  };

  return (
    <div style={{ padding: '20px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h2>{title}</h2>
        <div>
          <button 
            style={{ 
              padding: '8px 16px', 
              marginRight: '10px', 
              backgroundColor: viewMode === 'svid' ? '#0078d4' : '#f0f0f0',
              color: viewMode === 'svid' ? 'white' : 'black',
              border: '1px solid #ddd',
              borderRadius: '4px',
              cursor: 'pointer'
            }}
            onClick={() => setViewMode('svid')}
          >
            SVID Certificates
          </button>
          <button 
            style={{ 
              padding: '8px 16px',
              backgroundColor: viewMode === 'ca' ? '#0078d4' : '#f0f0f0',
              color: viewMode === 'ca' ? 'white' : 'black',
              border: '1px solid #ddd',
              borderRadius: '4px',
              cursor: 'pointer'
            }}
            onClick={() => setViewMode('ca')}
          >
            CA Bundle Certificates
          </button>
        </div>
      </div>
      
      <PeculiarCertificatesViewer
        certificates={certificates}
        onCertificateSelect={handleRowClick}
        hasDownload={true}
      />
    </div>
  );
}

export default CertDisplayTable;