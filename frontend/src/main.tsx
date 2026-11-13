import React from 'react'
import ReactDOM from 'react-dom/client'
// Fonts are bundled: the app runs offline. Latin subsets only.
import '@fontsource/inter/latin-400.css'
import '@fontsource/inter/latin-500.css'
import '@fontsource/inter/latin-600.css'
import '@fontsource/jetbrains-mono/latin-400.css'
import '@fontsource/jetbrains-mono/latin-600.css'
import '@fontsource/fraunces/latin-600-italic.css'
import '@fontsource/fredoka/latin-600.css'
import '@fontsource/nunito/latin-400.css'
import '@fontsource/nunito/latin-600.css'
import '@fontsource/nunito/latin-700.css'
import './themes.css'
import './app.css'
import App from './App'

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
