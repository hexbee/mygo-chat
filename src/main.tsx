import { createRoot } from 'react-dom/client'
import { isMyGo, runtime } from 'mygo-runtime'
import { App } from './app/App.js'
import { isMac } from './app/shortcuts.js'
import { setupTheme } from './app/theme.js'
import { ensureKatex } from './services/markdown/katex.js'
import './app/theme.css'
import './app/global.css'

document.documentElement.dataset.platform = isMyGo()
  ? runtime().platform
  : isMac
    ? 'darwin'
    : 'web'

ensureKatex()
setupTheme()

createRoot(document.getElementById('root') as HTMLElement).render(<App />)
