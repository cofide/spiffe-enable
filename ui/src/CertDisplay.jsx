// src/CertificateDisplay.js
import React from 'react';
import { PeculiarCertificateViewer } from '@peculiar/certificates-viewer-react';

const pemCertificate = `
-----BEGIN CERTIFICATE-----
-----END CERTIFICATE-----
`.trim();

function CertDisplay() {
  return (
    <div style={{ padding: '20px' }}>
      <h2>Certificate Viewer</h2>
      <PeculiarCertificateViewer
        certificate={pemCertificate} 
        download
      />
    </div>
  );
}

export default CertDisplay;
