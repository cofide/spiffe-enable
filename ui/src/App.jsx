// src/App.js
import React from 'react';
import './App.css'; // Or your main CSS file
import CertDisplayTable from './CertDisplayTable';

function App() {
  // In production, these values will be replaced with server-side rendered data
  // The Go server will inject the actual certificate values before serving the HTML
  const svidCertificates = window.SPIFFE_SVID_CERTIFICATES || [];
  const caBundleCertificates = window.SPIFFE_CA_BUNDLE_CERTIFICATES || [];
  
  return (
    <div className="App">
      <header className="App-header">
        <h1>SPIFFE Certificate Viewer</h1>
        <p>View SVID and Trust Bundle Certificates</p>
      </header>
      <main>
        <CertDisplayTable 
          svidCertificates={svidCertificates}
          caBundleCertificates={caBundleCertificates}
        />
      </main>
    </div>
  );
}

export default App;
