import { readFileSync } from 'node:fs'
import { defineConfig } from 'mygo-cli'

const pkg = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8')) as {
  version: string
}

export default defineConfig({
  name: 'Vela',
  slug: 'vela',
  identifier: 'io.hexbee.vela',
  version: pkg.version,
  copyright: '© 2026 Vela',

  devUrl: 'http://localhost:8090',
  devCommand: 'pnpm run dev:web',
  buildCommand: 'pnpm run build:web',
  frontendDist: 'dist',
  bindings: 'src/mygo.ts',
  out: 'build',

  updates: {
    publicKey: '9H5AdBh2JG1llhMVTMWcPKbWYdgfDiRKRqkYuQfIDOk=',
    github: 'hexbee/mygo-chat',
  },

  macos: {
    minimumSystemVersion: '12.0',
    infoPlist: {
      NSMicrophoneUsageDescription: 'Vela 需要麦克风权限以支持语音输入和实时录音功能。',
    },
  },
})
