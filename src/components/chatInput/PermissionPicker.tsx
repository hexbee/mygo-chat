import clsx from 'clsx'
import { useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useTranslate, type TranslationKey } from '../../i18n/index.js'
import { setSessionPermissionMode, useChatStore } from '../../services/chat/store.js'
import { useWorkspaceStore } from '../../services/workspace/store.js'
import type { PermissionMode } from '../../services/workspace/types.js'
import { useAnchoredPosition, useDismiss } from '../base/anchored.js'
import {
  IconCheckedOutline14,
  IconChevronDownOutline14,
  IconShieldFull14,
  IconShieldReadOnly14,
  IconShieldWrite14,
} from '../base/icons.js'
import styles from './ComposerPickers.module.css'

type PermissionOption = {
  mode: PermissionMode
  labelKey: TranslationKey
  descKey: TranslationKey
  icon: ReactNode
}

const PERMISSION_OPTIONS: readonly PermissionOption[] = [
  {
    mode: 'read-only',
    labelKey: 'permissionReadOnly',
    descKey: 'permissionReadOnlyDesc',
    icon: <IconShieldReadOnly14 />,
  },
  {
    mode: 'workspace-write',
    labelKey: 'permissionWorkspaceWrite',
    descKey: 'permissionWorkspaceWriteDesc',
    icon: <IconShieldWrite14 />,
  },
  {
    mode: 'full-access',
    labelKey: 'permissionFullAccess',
    descKey: 'permissionFullAccessDesc',
    icon: <IconShieldFull14 />,
  },
]

/** 输入框左下角三档权限下拉（1:1 对齐 DSH）：仅可查看 / 工作区内修改 / 完全权限 */
export const PermissionPicker = ({
  sessionId,
  disabled,
}: {
  sessionId: string | null
  disabled?: boolean
}) => {
  const t = useTranslate()
  const [open, setOpen] = useState(false)
  const draftPermissionMode = useWorkspaceStore((state) => state.draftPermissionMode)
  const sessionSummary = useChatStore((state) =>
    sessionId === null ? undefined : state.sessions.find((item) => item.id === sessionId)
  )
  const sessionDetail = useChatStore((state) =>
    sessionId !== null && Object.hasOwn(state.sessionCache, sessionId)
      ? state.sessionCache[sessionId]
      : undefined
  )

  const currentMode: PermissionMode =
    sessionId === null
      ? draftPermissionMode
      : (sessionDetail?.permissionMode ?? sessionSummary?.permissionMode ?? 'workspace-write')

  const currentOption =
    PERMISSION_OPTIONS.find((item) => item.mode === currentMode) ?? PERMISSION_OPTIONS[1]

  const { anchorRef, floatingRef, position } = useAnchoredPosition<HTMLSpanElement, HTMLDivElement>(
    open,
    { side: 'top', align: 'start', offset: 6 }
  )
  useDismiss(open, [anchorRef, floatingRef], () => setOpen(false))

  return (
    <>
      <span ref={anchorRef} className={styles.anchor}>
        <button
          type="button"
          className={clsx(
            styles.trigger,
            styles.triggerActive,
            currentMode === 'full-access' && styles.triggerFullAccess,
            open && styles.triggerOpen
          )}
          title={t(currentOption.descKey)}
          aria-label={`${t('permissionModeLabel')}: ${t(currentOption.labelKey)}`}
          aria-haspopup="menu"
          aria-expanded={open}
          disabled={disabled}
          onClick={() => setOpen((value) => !value)}
        >
          <span className={styles.triggerIcon}>{currentOption.icon}</span>
          <span className={styles.triggerLabel}>{t(currentOption.labelKey)}</span>
          <span className={clsx(styles.chevron, open && styles.chevronOpen)}>
            <IconChevronDownOutline14 />
          </span>
        </button>
      </span>
      {open
        ? createPortal(
            <div
              ref={floatingRef}
              className={clsx(styles.popover, styles.permissionPopover)}
              style={position ?? undefined}
              role="menu"
              aria-label={t('permissionModeLabel')}
            >
              {PERMISSION_OPTIONS.map((option) => {
                const selected = option.mode === currentMode
                return (
                  <button
                    key={option.mode}
                    type="button"
                    role="menuitemradio"
                    aria-checked={selected}
                    className={clsx(styles.option, selected && styles.optionSelected)}
                    onClick={() => {
                      setOpen(false)
                      setSessionPermissionMode(sessionId, option.mode)
                    }}
                  >
                    <span
                      className={clsx(
                        styles.optionIcon,
                        option.mode === 'full-access' && styles.optionIconWarn
                      )}
                    >
                      {option.icon}
                    </span>
                    <span className={styles.optionBody}>
                      <span className={styles.optionTitle}>{t(option.labelKey)}</span>
                      <span className={styles.optionDesc}>{t(option.descKey)}</span>
                    </span>
                    <span className={styles.check}>
                      {selected ? <IconCheckedOutline14 /> : null}
                    </span>
                  </button>
                )
              })}
            </div>,
            document.body
          )
        : null}
    </>
  )
}
