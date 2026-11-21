import { create } from 'zustand'
import {
  SkillService,
  type SkillCatalog,
  type SkillDocument,
  type SkillSummary,
} from '../../mygo.js'
import { useSettings } from '../settings.js'

type SkillState = {
  workdir: string
  skills: SkillSummary[]
  digest: string
  loading: boolean
}

export const useSkillStore = create<SkillState>(() => ({
  workdir: '',
  skills: [],
  digest: '',
  loading: false,
}))

const { getState: get, setState: set } = useSkillStore

let inFlightKey = ''
let inFlightTask: Promise<SkillCatalog> | null = null

const currentSkillConfig = () => useSettings.getState().skillConfig

/** 扫描当前工作区与用户目录下的可用 Skills，返回摘要与 digest */
export const refreshSkills = async (workdir: string): Promise<SkillCatalog> => {
  const normalized = workdir.trim()
  const cfg = currentSkillConfig()
  const key = `${normalized}|${JSON.stringify(cfg)}`
  if (inFlightTask && inFlightKey === key) {
    return inFlightTask
  }
  inFlightKey = key
  set({ loading: true })
  const task = SkillService.listSkills(normalized, cfg)
    .then((catalog) => {
      const skills = Array.isArray(catalog?.skills) ? catalog.skills : []
      const digest = catalog?.digest ?? ''
      set({
        workdir: normalized,
        skills,
        digest,
        loading: false,
      })
      return { skills, digest }
    })
    .catch(() => {
      set({ loading: false })
      return { skills: get().skills, digest: get().digest }
    })
    .finally(() => {
      if (inFlightTask === task) {
        inFlightTask = null
      }
    })
  inFlightTask = task
  return task
}

// 配置变动（目录增删、顺序调整、启停开关）时立即刷新当前工作区的 Skills，确保输入框 / 菜单实时生效。
useSettings.subscribe((state, prev) => {
  if (state.skillConfig !== prev.skillConfig) {
    void refreshSkills(get().workdir)
  }
})

/** 构造渐进式 Skills 摘要目录的 <system-reminder> 块（按需由模型调用 skill 工具读全文） */
export const buildSkillCatalogReminder = (catalog: SkillCatalog): string => {
  if (!catalog.skills.length) {
    return ''
  }
  const lines = catalog.skills.map((skill) => {
    const scopeTag = skill.scope === 'project' ? '[project]' : '[user]'
    const desc = skill.description ? `: ${skill.description}` : ''
    return `- ${skill.name} ${scopeTag}${desc}`
  })
  return [
    `<system-reminder type="skills-catalog" digest="${catalog.digest}">`,
    'Available local skills (progressive loading — only metadata is shown; call tool `skill(name)` to load full instructions when relevant):',
    ...lines,
    '</system-reminder>',
  ].join('\n')
}

/** 解析用户输入中所有的 `/skill-name` 显式调用（支持单条消息内同时引用多个技能） */
export const parseSlashSkillInvocations = (text: string): string[] => {
  const names: string[] = []
  const seen = new Set<string>()
  const regex = /(?:^|\s)\/([A-Za-z0-9._-]+)/g
  for (const match of text.matchAll(regex)) {
    const name = match[1]
    const key = name.toLowerCase()
    if (!seen.has(key)) {
      seen.add(key)
      names.push(name)
    }
  }
  return names
}

/** 如果用户输入包含 `/skill-name` 且匹配本地启用 Skill，预加载全文并返回注入的 <system-reminder> */
export const resolveExplicitSkillReminder = async (
  userText: string,
  workdir: string
): Promise<string> => {
  const skillNames = parseSlashSkillInvocations(userText)
  if (skillNames.length === 0) {
    return ''
  }
  const cfg = currentSkillConfig()
  const docs = await Promise.all(
    skillNames.map((name) =>
      SkillService.readSkill(name, workdir, cfg).catch(() => null as SkillDocument | null)
    )
  )
  const blocks: string[] = []
  for (const doc of docs) {
    if (!doc?.content) {
      continue
    }
    blocks.push(
      [
        `<system-reminder type="skill-invoked" name="${doc.name}" scope="${doc.scope}">`,
        `The user explicitly invoked skill "/${doc.name}". Follow these skill instructions carefully.`,
        `Base directory: ${doc.baseDir}`,
        '',
        doc.content,
        '</system-reminder>',
      ].join('\n')
    )
  }
  return blocks.join('\n\n')
}
