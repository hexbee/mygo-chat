import { create } from 'zustand'
import type { TtsVoice } from './api/speech.js'
import type { ChatModel, ReasoningEffort } from './api/types.js'
import { normalizeLocation, type UserLocation } from './location.js'
import type { PermissionMode } from './workspace/types.js'

export type Locale = 'zh-CN' | 'en-US'

/** 主题偏好；默认跟随系统 */
export type ThemePreference = 'light' | 'dark' | 'system'

/** 沙箱生效时（工作区内修改模式）默认需要人工审批的命令黑名单 */
export const DEFAULT_APPROVAL_BLACKLIST: readonly string[] = [
  'rm',
  'rmdir',
  'sudo',
  'su',
  'mkfs',
  'dd',
  'chmod',
  'chown',
  'shutdown',
  'reboot',
  'halt',
  'kill',
  'killall',
  'pkill',
  'diskutil',
  'format',
  'git reset',
  'git clean',
  'git checkout',
  'git restore',
  'git rebase',
  'git merge',
  'git push',
  'curl',
  'wget',
  'ssh',
  'scp',
  'rsync',
  'nc',
  'netcat',
  'telnet',
  'ftp',
  'sftp',
  'gh',
  'aws',
  'gcloud',
  'az',
  'twine',
  'npm publish',
  'pnpm publish',
  'yarn publish',
  'cargo publish',
]

/** 本机设置，存 localStorage：API Key、语言、主题、位置、朗读音色、模型、推理等级、权限默认值、审批黑名单、深度思考与智能搜索开关、侧边栏收起状态 */
type Settings = {
  apiKey: string | null
  locale: Locale
  themePreference: ThemePreference
  location: UserLocation | null
  ttsVoice: TtsVoice
  model: ChatModel
  reasoningEffort: ReasoningEffort
  defaultPermissionMode: PermissionMode
  approvalBlacklist: string[]
  thinking: boolean
  search: boolean
  siderCollapsed: boolean
}

const KEYS = {
  apiKey: 'chat-demo:api-key',
  locale: 'chat-demo:locale',
  location: 'chat-demo:location',
  themePreference: 'chat-demo:theme-preference',
  ttsVoice: 'chat-demo:tts-voice',
  model: 'chat-demo:model',
  reasoningEffort: 'chat-demo:reasoning-effort',
  permissionMode: 'chat-demo:permission-mode',
  approvalBlacklist: 'chat-demo:approval-blacklist',
  thinking: 'chat-demo:thinking',
  search: 'chat-demo:search',
  siderCollapsed: 'chat-demo:sider-collapsed',
} as const

const read = (key: string) => {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

const write = (key: string, value: string | null) => {
  try {
    if (value === null) {
      localStorage.removeItem(key)
    } else {
      localStorage.setItem(key, value)
    }
  } catch {
    // 存储不可用时只在内存中生效
  }
}

/** 位置选择模式跟随浏览器语言，与应用内选择的界面语言无关。 */
export const isChineseBrowser = (): boolean =>
  (typeof navigator === 'undefined' ? 'zh' : navigator.language).toLowerCase().startsWith('zh')

const initialLocale = (): Locale => {
  const saved = read(KEYS.locale)
  if (saved === 'zh-CN' || saved === 'en-US') {
    return saved
  }
  return isChineseBrowser() ? 'zh-CN' : 'en-US'
}

const initialLocation = (): UserLocation | null => {
  try {
    const location = normalizeLocation(JSON.parse(read(KEYS.location) ?? 'null'))
    if (!location) {
      return null
    }
    return isChineseBrowser()
      ? location.region === 'CN' && location.province
        ? location
        : null
      : { region: location.region }
  } catch {
    return null
  }
}

/** 未保存过时跟随系统 */
const initialThemePreference = (): ThemePreference => {
  const saved = read(KEYS.themePreference)
  return saved === 'light' || saved === 'dark' ? saved : 'system'
}

/** 未保存过时使用服务端默认音色 stella（海星） */
const initialTtsVoice = (): TtsVoice => (read(KEYS.ttsVoice) === 'tide' ? 'tide' : 'stella')

/** 未保存过时使用默认模型 deepseek-flash */
const initialModel = (): ChatModel =>
  read(KEYS.model) === 'deepseek-v4-pro' ? 'deepseek-v4-pro' : 'deepseek-flash'

/** 未保存过时兼容旧版 thinking 开关：开启对应 high，关闭对应 off */
const initialReasoningEffort = (): ReasoningEffort => {
  const saved = read(KEYS.reasoningEffort)
  if (saved === 'off' || saved === 'low' || saved === 'high' || saved === 'max') {
    return saved
  }
  return read(KEYS.thinking) === 'true' ? 'high' : 'off'
}

/** 未保存过时默认权限档位为工作区内修改 */
const initialPermissionMode = (): PermissionMode => {
  const saved = read(KEYS.permissionMode)
  if (saved === 'read-only' || saved === 'workspace-write' || saved === 'full-access') {
    return saved
  }
  return 'workspace-write'
}

const normalizeBlacklist = (items: readonly string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of items) {
    const cleaned = item.trim().replace(/\s+/g, ' ')
    if (!cleaned) {
      continue
    }
    const key = cleaned.toLowerCase()
    if (!seen.has(key)) {
      seen.add(key)
      out.push(cleaned)
    }
  }
  return out
}

const initialApprovalBlacklist = (): string[] => {
  const raw = read(KEYS.approvalBlacklist)
  if (raw === null) {
    return [...DEFAULT_APPROVAL_BLACKLIST]
  }
  try {
    const parsed: unknown = JSON.parse(raw)
    if (Array.isArray(parsed)) {
      return normalizeBlacklist(parsed.filter((item): item is string => typeof item === 'string'))
    }
  } catch {
    // fall back to defaults
  }
  return [...DEFAULT_APPROVAL_BLACKLIST]
}

const bootReasoningEffort = initialReasoningEffort()

export const useSettings = create<Settings>(() => ({
  apiKey: read(KEYS.apiKey),
  locale: initialLocale(),
  location: initialLocation(),
  themePreference: initialThemePreference(),
  ttsVoice: initialTtsVoice(),
  model: initialModel(),
  reasoningEffort: bootReasoningEffort,
  defaultPermissionMode: initialPermissionMode(),
  approvalBlacklist: initialApprovalBlacklist(),
  thinking: bootReasoningEffort !== 'off',
  search: read(KEYS.search) === 'true',
  siderCollapsed: read(KEYS.siderCollapsed) === 'true',
}))

export const setLocale = (locale: Locale) => {
  write(KEYS.locale, locale)
  useSettings.setState({ locale })
}

export const setLocation = (value: UserLocation | null) => {
  const location = normalizeLocation(value)
  write(KEYS.location, location ? JSON.stringify(location) : null)
  useSettings.setState({ location })
}

export const setThemePreference = (themePreference: ThemePreference) => {
  write(KEYS.themePreference, themePreference)
  useSettings.setState({ themePreference })
}

export const setTtsVoice = (ttsVoice: TtsVoice) => {
  write(KEYS.ttsVoice, ttsVoice)
  useSettings.setState({ ttsVoice })
}

export const setApiKey = (apiKey: string | null) => {
  write(KEYS.apiKey, apiKey)
  useSettings.setState({ apiKey })
}

export const setModel = (model: ChatModel) => {
  write(KEYS.model, model)
  useSettings.setState({ model })
}

export const setReasoningEffort = (reasoningEffort: ReasoningEffort) => {
  const thinking = reasoningEffort !== 'off'
  write(KEYS.reasoningEffort, reasoningEffort)
  write(KEYS.thinking, String(thinking))
  useSettings.setState({ reasoningEffort, thinking })
}

export const setDefaultPermissionMode = (defaultPermissionMode: PermissionMode) => {
  write(KEYS.permissionMode, defaultPermissionMode)
  useSettings.setState({ defaultPermissionMode })
}

export const setApprovalBlacklist = (rules: readonly string[]) => {
  const normalized = normalizeBlacklist(rules)
  write(KEYS.approvalBlacklist, JSON.stringify(normalized))
  useSettings.setState({ approvalBlacklist: normalized })
}

export const resetApprovalBlacklist = () => {
  write(KEYS.approvalBlacklist, null)
  useSettings.setState({ approvalBlacklist: [...DEFAULT_APPROVAL_BLACKLIST] })
}

export const setThinking = (thinking: boolean) => {
  setReasoningEffort(thinking ? 'high' : 'off')
}

export const setSearch = (search: boolean) => {
  write(KEYS.search, String(search))
  useSettings.setState({ search })
}

export const setSiderCollapsed = (siderCollapsed: boolean) => {
  write(KEYS.siderCollapsed, String(siderCollapsed))
  useSettings.setState({ siderCollapsed })
}
