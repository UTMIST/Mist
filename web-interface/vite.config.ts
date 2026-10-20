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

function docsStaticPlugin(): Plugin {
  return {
    name: 'mist-docs-static',
    buildStart() {
      const docsIndex = path.resolve(__dirname, 'public/docs/index.html')
      if (!fs.existsSync(docsIndex)) {
        try {
          execSync('hugo -s ../docs -d ../web-interface/public/docs', {
            stdio: 'ignore',
          })
        } catch {
          // Hugo may not be installed; ignore build error
        }
      }
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (!req.url) return next()
        const [pathname, search] = req.url.split('?')
        const query = search ? `?${search}` : ''
        if (pathname === '/docs') {
          res.writeHead(301, { Location: `/docs/${query}` })
          res.end()
          return
        }
        if (pathname === '/docs/') {
          req.url = `/docs/index.html${query}`
          return next()
        }
        if (pathname.startsWith('/docs/')) {
          const relative = pathname.replace(/^\/docs\//, '').replace(/\/$/, '')
          const potentialIndex = path.resolve(
            __dirname,
            'public/docs',
            relative,
            'index.html',
          )
          if (fs.existsSync(potentialIndex)) {
            req.url = `/docs/${relative}/index.html${query}`
          }
        }
        next()
      })
    },
    configurePreviewServer(server) {
      server.middlewares.use((req, res, next) => {
        if (!req.url) return next()
        const [pathname, search] = req.url.split('?')
        const query = search ? `?${search}` : ''
        if (pathname === '/docs') {
          res.writeHead(301, { Location: `/docs/${query}` })
          res.end()
          return
        }
        if (pathname === '/docs/') {
          req.url = `/docs/index.html${query}`
          return next()
        }
        if (pathname.startsWith('/docs/')) {
          const relative = pathname.replace(/^\/docs\//, '').replace(/\/$/, '')
          const potentialIndex = path.resolve(
            __dirname,
            'dist/docs',
            relative,
            'index.html',
          )
          if (fs.existsSync(potentialIndex)) {
            req.url = `/docs/${relative}/index.html${query}`
          }
        }
        next()
      })
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
