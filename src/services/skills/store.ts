import { create } from 'zustand'
import { SkillService, type SkillCatalog, type SkillDocument, type SkillSummary } from '../../mygo.js'

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

/** 扫描当前工作区与用户目录下的可用 Skills，返回摘要与 digest */
export const refreshSkills = async (workdir: string): Promise<SkillCatalog> => {
  const normalized = workdir.trim()
  if (inFlightTask && inFlightKey === normalized) {
    return inFlightTask
  }
  inFlightKey = normalized
  set({ loading: true })
  const task = SkillService.listSkills(normalized)
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

/** 解析用户输入开头的 `/skill-name` 显式调用 */
export const parseSlashSkillInvocation = (text: string): string | null => {
  const match = text.trimStart().match(/^\/([A-Za-z0-9._-]+)(?:\s|$)/)
  return match ? match[1] : null
}

/** 如果用户输入以 `/skill-name` 开头且匹配本地 Skill，预加载全文并返回注入的 <system-reminder> */
export const resolveExplicitSkillReminder = async (
  userText: string,
  workdir: string
): Promise<string> => {
  const skillName = parseSlashSkillInvocation(userText)
  if (!skillName) {
    return ''
  }
  try {
    const doc: SkillDocument = await SkillService.readSkill(skillName, workdir)
    if (!doc?.content) {
      return ''
    }
    return [
      `<system-reminder type="skill-invoked" name="${doc.name}" scope="${doc.scope}">`,
      `The user explicitly invoked skill "/${doc.name}". Follow these skill instructions carefully.`,
      `Base directory: ${doc.baseDir}`,
      '',
      doc.content,
      '</system-reminder>',
    ].join('\n')
  } catch {
    return ''
  }
}
