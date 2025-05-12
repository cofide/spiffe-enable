// src/App.js
import React from 'react';
import './App.css'; // Or your main CSS file
import CertDisplay from './CertDisplay';

function App() {
  return (
    <div className="App">
      <header className="App-header">
        <h1>Certificate Viewer</h1>
      </header>
      <main>
        <CertDisplay />
      </main>
    </div>
  );
}

export default App;
