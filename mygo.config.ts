import { defineConfig } from 'mygo-cli'

export default defineConfig({
  name: 'DeepSeek Chat',
  slug: 'deepseek-chat',
  identifier: 'com.deepseek.chat',
  version: '0.1.8',
  copyright: '© 2026 DeepSeek Chat',

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
      NSMicrophoneUsageDescription: 'DeepSeek Chat 需要麦克风权限以支持语音输入和实时录音功能。',
    },
  },
})
