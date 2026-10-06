import path from 'path'
import { fileURLToPath } from 'url'
import type { NextConfig } from 'next'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const nextConfig: NextConfig = {
  // Parallel previews must use their own build directory to avoid overwriting the desktop dev server's chunks.
  distDir: process.env.AUTOCARTO_NEXT_DIST_DIR || '.next',
  output: 'export',
  images: { unoptimized: true },
  outputFileTracingRoot: __dirname,
}

export default nextConfig
