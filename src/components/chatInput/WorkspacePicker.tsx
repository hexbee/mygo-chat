import clsx from 'clsx'
import { useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslate } from '../../i18n/index.js'
import { setSessionWorkspace, useChatStore } from '../../services/chat/store.js'
import {
  distinguishingPathSuffixes,
  openWorkspacePath,
  pickAndAddWorkspace,
  useWorkspaceStore,
} from '../../services/workspace/store.js'
import { useAnchoredPosition, useDismiss } from '../base/anchored.js'
import {
  IconCheckedOutline14,
  IconChevronDownOutline14,
  IconCloseOutline16,
  IconFolderOpenOutline14,
  IconFolderOutline14,
  IconPlusOutline14,
} from '../base/icons.js'
import { showToast } from '../base/toastState.js'
import styles from './ComposerPickers.module.css'

/**
 * 输入框左上角工作区控件（对齐 DSH）：
 * - 新对话（sessionId === null）：可交互下拉，用于为即将创建的新会话选择/添加/取消工作区；
 * - 已有会话（sessionId !== null）：会话归属工作区已锁定，仅只读展示所属工作区或临时目录（点击打开目录），不允许把已有会话迁移到其他工作区。
 */
export const WorkspacePicker = ({
  sessionId,
  disabled,
}: {
  sessionId: string | null
  disabled?: boolean
}) => {
  const t = useTranslate()
  const [open, setOpen] = useState(false)
  const [picking, setPicking] = useState(false)
  const workspaces = useWorkspaceStore((state) => state.workspaces)
  const draftWorkspaceId = useWorkspaceStore((state) => state.draftWorkspaceId)
  const sessionSummary = useChatStore((state) =>
    sessionId === null ? undefined : state.sessions.find((item) => item.id === sessionId)
  )
  const sessionDetail = useChatStore((state) =>
    sessionId !== null && Object.hasOwn(state.sessionCache, sessionId)
      ? state.sessionCache[sessionId]
      : undefined
  )

  const activeWorkspaceId =
    sessionId === null
      ? draftWorkspaceId
      : (sessionDetail?.workspaceId ?? sessionSummary?.workspaceId ?? null)
  const scratchDir =
    sessionId === null ? null : (sessionDetail?.scratchDir ?? sessionSummary?.scratchDir ?? null)

  const activeWorkspace = useMemo(
    () => (activeWorkspaceId ? workspaces.find((w) => w.id === activeWorkspaceId) ?? null : null),
    [activeWorkspaceId, workspaces]
  )
  const suffixes = useMemo(() => distinguishingPathSuffixes(workspaces), [workspaces])

  const isLockedSession = sessionId !== null

  const { anchorRef, floatingRef, position } = useAnchoredPosition<HTMLSpanElement, HTMLDivElement>(
    open && !isLockedSession,
    { side: 'bottom', align: 'start', offset: 6 }
  )
  useDismiss(open && !isLockedSession, [anchorRef, floatingRef], () => setOpen(false))

  // 已有会话：工作区归属锁定，不提供切换下拉，避免把当前会话迁移到其他工作区
  if (isLockedSession) {
    const lockedPath = activeWorkspace?.path ?? scratchDir
    if (!lockedPath) {
      return null
    }
    const lockedLabel = activeWorkspace ? activeWorkspace.name : t('workspaceScratchDir')
    return (
      <div className={styles.topRow} onClick={(event) => event.stopPropagation()}>
        <button
          type="button"
          className={clsx(styles.trigger, styles.triggerActive)}
          title={`${t('workspaceOpenDir')}: ${lockedPath}`}
          disabled={disabled}
          onClick={() => void openWorkspacePath(lockedPath)}
        >
          <span className={styles.triggerIcon}>
            <IconFolderOpenOutline14 />
          </span>
          <span className={styles.triggerLabel}>{lockedLabel}</span>
        </button>
      </div>
    )
  }

  const handleAddWorkspace = async () => {
    setOpen(false)
    if (picking) {
      return
    }
    setPicking(true)
    try {
      const workspace = await pickAndAddWorkspace()
      if (workspace) {
        setSessionWorkspace(null, workspace.id)
      }
    } catch {
      showToast({ key: 'workspacePickFailed', type: 'error' })
    } finally {
      setPicking(false)
    }
  }

  const label = activeWorkspace ? activeWorkspace.name : t('workspaceNoneSelected')
  const tooltip = activeWorkspace?.path ?? t('workspaceNoneSelected')

  return (
    <div className={styles.topRow} onClick={(event) => event.stopPropagation()}>
      <span ref={anchorRef} className={styles.anchor}>
        <button
          type="button"
          className={clsx(
            styles.trigger,
            activeWorkspace && styles.triggerActive,
            open && styles.triggerOpen
          )}
          title={tooltip}
          aria-haspopup="menu"
          aria-expanded={open}
          disabled={disabled || picking}
          onClick={() => setOpen((value) => !value)}
        >
          <span className={styles.triggerIcon}>
            {activeWorkspace ? <IconFolderOpenOutline14 /> : <IconFolderOutline14 />}
          </span>
          <span className={styles.triggerLabel}>{label}</span>
          <span className={clsx(styles.chevron, open && styles.chevronOpen)}>
            <IconChevronDownOutline14 />
          </span>
        </button>
      </span>
      {open
        ? createPortal(
            <div
              ref={floatingRef}
              className={styles.popover}
              style={position ?? undefined}
              role="menu"
            >
              {workspaces.length > 0 ? (
                <>
                  <div className={styles.list}>
                    {workspaces.map((workspace) => {
                      const selected = workspace.id === activeWorkspace?.id
                      const suffix = suffixes.get(workspace.id)
                      return (
                        <button
                          key={workspace.id}
                          type="button"
                          role="menuitemradio"
                          aria-checked={selected}
                          className={clsx(styles.option, selected && styles.optionSelected)}
                          title={workspace.path}
                          onClick={() => {
                            setOpen(false)
                            setSessionWorkspace(null, workspace.id)
                          }}
                        >
                          <span className={styles.optionIcon}>
                            <IconFolderOutline14 />
                          </span>
                          <span className={styles.optionBody}>
                            <span className={styles.optionTitleRow}>
                              <span className={styles.optionTitle}>{workspace.name}</span>
                              {suffix ? <span className={styles.optionSuffix}>{suffix}</span> : null}
                            </span>
                          </span>
                          <span className={styles.check}>
                            {selected ? <IconCheckedOutline14 /> : null}
                          </span>
                        </button>
                      )
                    })}
                  </div>
                  {activeWorkspace ? (
                    <button
                      type="button"
                      role="menuitem"
                      className={styles.option}
                      onClick={() => {
                        setOpen(false)
                        setSessionWorkspace(null, null)
                      }}
                    >
                      <span className={styles.optionIcon}>
                        <IconCloseOutline16 />
                      </span>
                      <span className={styles.optionBody}>
                        <span className={styles.optionTitle}>{t('workspaceUnselect')}</span>
                      </span>
                    </button>
                  ) : null}
                  <div className={styles.divider} />
                </>
              ) : null}
              <button
                type="button"
                role="menuitem"
                className={styles.option}
                disabled={picking}
                onClick={() => void handleAddWorkspace()}
              >
                <span className={styles.optionIcon}>
                  <IconPlusOutline14 />
                </span>
                <span className={styles.optionBody}>
                  <span className={styles.optionTitle}>{t('workspaceAddEllipsis')}</span>
                </span>
              </button>
            </div>,
            document.body
          )
        : null}
    </div>
  )
}
