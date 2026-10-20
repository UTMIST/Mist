import { defineConfig } from 'vite'
import type { Plugin } from 'vite'
import { devtools } from '@tanstack/devtools-vite'
import tsconfigPaths from 'vite-tsconfig-paths'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { execSync } from 'node:child_process'

import { tanstackRouter } from '@tanstack/router-plugin/vite'

import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const MIME_TYPES: Record<string, string> = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.mjs': 'application/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.xml': 'application/xml; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.webp': 'image/webp',
  '.ico': 'image/x-icon',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.txt': 'text/plain; charset=utf-8',
}

function docsStaticPlugin(): Plugin {
  const docsDir = path.resolve(__dirname, '../docs/public')

  function ensureDocsBuilt() {
    if (!fs.existsSync(path.resolve(docsDir, 'index.html'))) {
      try {
        execSync('hugo -s ../docs', { stdio: 'ignore' })
      } catch {
        // Hugo may not be installed in minimal test environments
      }
    }
  }

  function serveFromDocs(
    req: { url?: string },
    res: {
      writeHead: (status: number, headers?: Record<string, string>) => void
      end: () => void
    },
    next: () => void,
  ) {
    if (!req.url) return next()
    const [pathname, search] = req.url.split('?')
    const query = search ? `?${search}` : ''

    if (pathname === '/docs') {
      res.writeHead(301, { Location: `/docs/${query}` })
      res.end()
      return
    }

    if (pathname.startsWith('/docs/')) {
      const relPath = pathname.replace(/^\/docs\//, '')
      let candidate = path.resolve(docsDir, relPath)

      // Prevent directory traversal outside docsDir
      if (!candidate.startsWith(docsDir)) {
        res.writeHead(403)
        res.end()
        return
      }

      if (fs.existsSync(candidate)) {
        const stat = fs.statSync(candidate)
        if (stat.isDirectory()) {
          if (!pathname.endsWith('/')) {
            res.writeHead(301, { Location: `${pathname}/${query}` })
            res.end()
            return
          }
          candidate = path.resolve(candidate, 'index.html')
        }
      } else {
        const asHtml = `${candidate}.html`
        if (fs.existsSync(asHtml)) candidate = asHtml
      }

      if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
        const ext = path.extname(candidate).toLowerCase()
        const mime = MIME_TYPES[ext] ?? 'application/octet-stream'
        res.writeHead(200, {
          'Content-Type': mime,
          'Cache-Control': 'no-cache',
        })
        fs.createReadStream(candidate).pipe(res as any)
        return
      }
    }

    next()
  }

  return {
    name: 'mist-docs-static',
    buildStart() {
      ensureDocsBuilt()
    },
    closeBundle() {
      ensureDocsBuilt()
      const distDocs = path.resolve(__dirname, 'dist/docs')
      if (fs.existsSync(docsDir)) {
        fs.cpSync(docsDir, distDocs, { recursive: true })
      }
    },
    configureServer(server) {
      ensureDocsBuilt()
      server.middlewares.use(serveFromDocs)
    },
  }
}

const config = defineConfig({
  server: {
    proxy: {
      '/auth': {
        target: process.env.MIST_API_PROXY ?? 'http://127.0.0.1:3000',
      },
      '/api': {
        target: process.env.MIST_API_PROXY ?? 'http://127.0.0.1:3000',
        rewrite: (p) => p.replace(/^\/api/, ''),
      },
    },
  },
  plugins: [
    devtools(),
    tsconfigPaths({ projects: ['./tsconfig.json'] }),
    tailwindcss(),
    docsStaticPlugin(),
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    viteReact(),
  ],
})

export default config
