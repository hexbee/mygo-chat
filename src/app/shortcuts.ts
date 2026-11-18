import { useEffect } from 'react'
import { newChat } from '../services/chat/store.js'
import { setSiderCollapsed, useSettings } from '../services/settings.js'
import {
  closeSettings,
  openSettings,
  useSettingsDialog,
} from '../components/settings/settingsDialogState.js'

export const isMac =
  typeof navigator !== 'undefined' &&
  (navigator.userAgent.includes('Macintosh') || (navigator as { userAgentData?: { platform?: string } }).userAgentData?.platform === 'macOS')

export const modKey = isMac ? '⌘' : 'Ctrl+'

/**
 * 桌面端全局快捷键支持：
 * - Cmd/Ctrl + N: 开启新对话
 * - Cmd/Ctrl + ,: 切换设置面板
 * - Cmd/Ctrl + B: 切换展开/收起侧边栏
 */
export const useGlobalShortcuts = () => {
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing) return

      const isModifier = e.metaKey || e.ctrlKey
      if (!isModifier || e.altKey) return

      const key = e.key.toLowerCase()

      // Cmd/Ctrl + N: 开启新会话
      if (key === 'n' && !e.shiftKey) {
        e.preventDefault()
        newChat()
        return
      }

      // Cmd/Ctrl + ,: 打开/关闭设置
      if (e.key === ',' && !e.shiftKey) {
        e.preventDefault()
        if (useSettingsDialog.getState().open) {
          closeSettings()
        } else {
          openSettings('general')
        }
        return
      }

      // Cmd/Ctrl + B: 切换侧边栏展开/收起
      if (key === 'b' && !e.shiftKey) {
        e.preventDefault()
        const current = useSettings.getState().siderCollapsed
        setSiderCollapsed(!current)
        return
      }
    }

    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [])
}
