import clsx from 'clsx'
import { useCallback, useEffect, useState } from 'react'
import { useTranslate } from '../../i18n/index.js'
import {
  SkillService,
  WorkspaceService,
  type SkillDirReport,
  type SkillInspection,
} from '../../mygo.js'
import { useChatStore } from '../../services/chat/store.js'
import {
  resetSkillConfig,
  setSkillProjectDirs,
  setSkillUserDirs,
  toggleSkillEnabled,
  useSettings,
} from '../../services/settings.js'
import { useWorkspaceStore } from '../../services/workspace/store.js'
import { Button } from '../base/Button.js'
import { showToast } from '../base/toastState.js'
import styles from './SkillsSetting.module.css'

const moveItem = (items: readonly string[], index: number, delta: -1 | 1): string[] => {
  const target = index + delta
  if (target < 0 || target >= items.length) {
    return [...items]
  }
  const next = [...items]
  const [moved] = next.splice(index, 1)
  next.splice(target, 0, moved)
  return next
}

export const SkillsSetting = () => {
  const t = useTranslate()
  const skillConfig = useSettings((state) => state.skillConfig)
  const activeId = useChatStore((state) => state.activeId)
  const activeSession = useChatStore((state) =>
    activeId
      ? (state.sessionCache[activeId] ?? state.sessions.find((s) => s.id === activeId))
      : undefined
  )
  const workspaces = useWorkspaceStore((state) => state.workspaces)
  const draftWorkspaceId = useWorkspaceStore((state) => state.draftWorkspaceId)
  const activeWorkspaceId = activeId ? (activeSession?.workspaceId ?? null) : draftWorkspaceId
  const activeWorkdir = workspaces.find((w) => w.id === activeWorkspaceId)?.path ?? ''

  const [inspection, setInspection] = useState<SkillInspection | null>(null)
  const [loading, setLoading] = useState(false)
  const [projectDraft, setProjectDraft] = useState('')
  const [userDraft, setUserDraft] = useState('')

  const runInspect = useCallback(async () => {
    setLoading(true)
    try {
      const res = await SkillService.inspectSkills(activeWorkdir, skillConfig)
      setInspection(res)
    } catch {
      setInspection(null)
    } finally {
      setLoading(false)
    }
  }, [activeWorkdir, skillConfig])

  useEffect(() => {
    void runInspect()
  }, [runInspect])

  const projectReports = (inspection?.directories ?? []).filter((d) => d.scope === 'project')
  const userReports = (inspection?.directories ?? []).filter((d) => d.scope === 'user')

  const handleAddProjectDir = () => {
    const trimmed = projectDraft.trim()
    if (!trimmed) {
      return
    }
    setSkillProjectDirs([...skillConfig.projectDirs, trimmed])
    setProjectDraft('')
  }

  const handleAddUserDir = () => {
    const trimmed = userDraft.trim()
    if (!trimmed) {
      return
    }
    setSkillUserDirs([...skillConfig.userDirs, trimmed])
    setUserDraft('')
  }

  const handlePickUserDir = async () => {
    try {
      const selected = await WorkspaceService.pickDirectory()
      if (selected) {
        setSkillUserDirs([...skillConfig.userDirs, selected])
      }
    } catch {
      showToast(t('workspacePickFailed'))
    }
  }

  const handleReset = () => {
    resetSkillConfig()
    showToast(t('settingsSkillsResetToast'))
  }

  return (
    <div className={styles.root}>
      <div className={styles.headerRow}>
        <div className={styles.headerText}>
          <div className={styles.title}>{t('settingsSkillsTitle')}</div>
          <div className={styles.desc}>{t('settingsSkillsDescription')}</div>
        </div>
        <div className={styles.headerActions}>
          <Button
            variant="outlined"
            size="m"
            className={styles.actionBtn}
            disabled={loading}
            onClick={() => void runInspect()}
          >
            {t('settingsSkillsRefresh')}
          </Button>
          <Button variant="outlined" size="m" className={styles.actionBtn} onClick={handleReset}>
            {t('settingsBlacklistReset')}
          </Button>
        </div>
      </div>

      {/* 项目级目录（优先） */}
      <div className={styles.group}>
        <div className={styles.groupHeader}>
          <span className={styles.groupTitle}>{t('settingsSkillsProjectSection')}</span>
          <span className={styles.groupHint} title={activeWorkdir || undefined}>
            {activeWorkdir
              ? t('settingsSkillsActiveWorkspace', { path: activeWorkdir })
              : t('settingsSkillsNoWorkspace')}
          </span>
        </div>
        <div className={styles.addRow}>
          <input
            type="text"
            className={styles.input}
            value={projectDraft}
            placeholder={t('settingsSkillsProjectPlaceholder')}
            onChange={(event) => setProjectDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                handleAddProjectDir()
              }
            }}
          />
          <Button
            variant="outlined"
            size="m"
            className={styles.actionBtn}
            disabled={!projectDraft.trim()}
            onClick={handleAddProjectDir}
          >
            {t('settingsBlacklistAdd')}
          </Button>
        </div>
        <DirectoryList
          dirs={skillConfig.projectDirs}
          reports={projectReports}
          priorityOffset={0}
          hasWorkspace={Boolean(activeWorkdir)}
          isProject
          onMove={(idx, delta) => setSkillProjectDirs(moveItem(skillConfig.projectDirs, idx, delta))}
          onRemove={(idx) =>
            setSkillProjectDirs(skillConfig.projectDirs.filter((_, i) => i !== idx))
          }
        />
      </div>

      {/* 全局目录 */}
      <div className={styles.group}>
        <div className={styles.groupHeader}>
          <span className={styles.groupTitle}>{t('settingsSkillsUserSection')}</span>
          <span className={styles.groupHint}>{t('settingsSkillsPriorityHint')}</span>
        </div>
        <div className={styles.addRow}>
          <input
            type="text"
            className={styles.input}
            value={userDraft}
            placeholder={t('settingsSkillsUserPlaceholder')}
            onChange={(event) => setUserDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                handleAddUserDir()
              }
            }}
          />
          <Button
            variant="outlined"
            size="m"
            className={styles.actionBtn}
            disabled={!userDraft.trim()}
            onClick={handleAddUserDir}
          >
            {t('settingsBlacklistAdd')}
          </Button>
          <Button
            variant="outlined"
            size="m"
            className={styles.actionBtn}
            onClick={() => void handlePickUserDir()}
          >
            {t('settingsSkillsBrowse')}
          </Button>
        </div>
        <DirectoryList
          dirs={skillConfig.userDirs}
          reports={userReports}
          priorityOffset={skillConfig.projectDirs.length}
          hasWorkspace
          isProject={false}
          onMove={(idx, delta) => setSkillUserDirs(moveItem(skillConfig.userDirs, idx, delta))}
          onRemove={(idx) => setSkillUserDirs(skillConfig.userDirs.filter((_, i) => i !== idx))}
        />
      </div>
    </div>
  )
}

const DirectoryList = ({
  dirs,
  reports,
  priorityOffset,
  hasWorkspace,
  isProject,
  onMove,
  onRemove,
}: {
  dirs: string[]
  reports: SkillDirReport[]
  priorityOffset: number
  hasWorkspace: boolean
  isProject: boolean
  onMove: (index: number, delta: -1 | 1) => void
  onRemove: (index: number) => void
}) => {
  const t = useTranslate()

  if (dirs.length === 0) {
    return <div className={styles.empty}>{t('settingsSkillsEmptyDirs')}</div>
  }

  return (
    <div className={styles.dirList}>
      {dirs.map((dir, index) => {
        const report = reports[index]
        const skills = report?.skills ?? []
        const exists = report?.exists ?? false
        const statusText =
          isProject && !hasWorkspace
            ? t('settingsSkillsNeedWorkspace')
            : !exists
              ? t('settingsSkillsDirMissing')
              : t('settingsSkillsCount', { count: skills.length })

        return (
          <div key={`${dir}-${index}`} className={styles.dirCard}>
            <div className={styles.dirHeader}>
              <span className={styles.rank}>#{priorityOffset + index + 1}</span>
              <span className={styles.dirPath} title={report?.resolvedPath || dir}>
                {dir}
              </span>
              <span className={clsx(styles.dirStatus, !exists && styles.dirStatusMuted)}>
                {statusText}
              </span>
              <div className={styles.dirControls}>
                <button
                  type="button"
                  className={styles.iconBtn}
                  disabled={index === 0}
                  title={t('settingsSkillsMoveUp')}
                  onClick={() => onMove(index, -1)}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className={styles.iconBtn}
                  disabled={index === dirs.length - 1}
                  title={t('settingsSkillsMoveDown')}
                  onClick={() => onMove(index, 1)}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className={clsx(styles.iconBtn, styles.iconBtnDanger)}
                  title={t('delete')}
                  onClick={() => onRemove(index)}
                >
                  ×
                </button>
              </div>
            </div>

            {skills.length > 0 ? (
              <div className={styles.skillList}>
                {skills.map((skill) => {
                  const shadowed = Boolean(skill.overriddenBy)
                  return (
                    <div
                      key={skill.path}
                      className={clsx(
                        styles.skillRow,
                        (!skill.enabled || shadowed) && styles.skillRowMuted
                      )}
                    >
                      <div className={styles.skillInfo}>
                        <div className={styles.skillNameRow}>
                          <span className={styles.skillName}>{skill.name}</span>
                          {shadowed ? (
                            <span className={styles.overrideBadge}>
                              {t('settingsSkillsOverriddenBy', { dir: skill.overriddenBy ?? '' })}
                            </span>
                          ) : null}
                        </div>
                        {skill.description ? (
                          <div className={styles.skillDesc} title={skill.description}>
                            {skill.description}
                          </div>
                        ) : null}
                      </div>
                      <button
                        type="button"
                        role="switch"
                        aria-checked={skill.enabled}
                        aria-label={`${skill.name} ${skill.enabled ? 'on' : 'off'}`}
                        className={clsx(styles.switch, skill.enabled && styles.switchOn)}
                        onClick={() => toggleSkillEnabled(skill.name, !skill.enabled)}
                      >
                        <span className={styles.switchThumb} />
                      </button>
                    </div>
                  )
                })}
              </div>
            ) : null}
          </div>
        )
      })}
    </div>
  )
}
