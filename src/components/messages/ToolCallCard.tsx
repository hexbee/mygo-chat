import clsx from 'clsx'
import { memo, useEffect, useState } from 'react'
import { useTranslate } from '../../i18n/index.js'
import { useChatStore } from '../../services/chat/store.js'
import type { ClientToolStep } from '../../services/chat/view.js'
import {
  basenameOfPath,
  openWorkspacePath,
  revealWorkspaceInFinder,
} from '../../services/workspace/store.js'
import { IconChevronDownOutline14, IconChevronRightOutline14 } from '../base/icons.js'
import { ApprovalCard } from './ApprovalCard.js'
import { DiffView } from './DiffView.js'
import styles from './ToolCallCard.module.css'

const summarizeToolCall = (name: string, input: Record<string, unknown>): string => {
  const str = (key: string) => (typeof input[key] === 'string' ? input[key] : '')
  switch (name) {
    case 'read':
    case 'write':
    case 'edit':
    case 'delete_file':
      return str('file_path')
    case 'glob':
      return str('path') ? `${str('pattern')} (${str('path')})` : str('pattern')
    case 'grep':
      return str('path') ? `${str('pattern')} in ${str('path')}` : str('pattern')
    case 'bash':
      return str('description') || str('command')
    default:
      return ''
  }
}

export const ToolCallCard = memo(function ToolCallCard({
  step,
  isLast,
}: {
  step: ClientToolStep
  isLast: boolean
}) {
  const t = useTranslate()
  const pendingApproval = useChatStore((state) =>
    Object.hasOwn(state.pendingApprovals, step.use.id)
      ? state.pendingApprovals[step.use.id]
      : undefined
  )
  const hasDiffs = Boolean(step.result?.uiMeta?.diffs?.length)
  const [expanded, setExpanded] = useState(hasDiffs)

  useEffect(() => {
    if (hasDiffs) {
      setExpanded(true)
    }
  }, [hasDiffs])

  const summary = summarizeToolCall(step.use.name, step.use.input)
  const diffs = step.result?.uiMeta?.diffs ?? []
  const producedFiles = step.result?.uiMeta?.producedFiles ?? []
  const exitCode = step.result?.uiMeta?.exitCode

  const statusLabel = pendingApproval
    ? t('toolApprovalStatusPending')
    : step.status === 'wip'
      ? t('toolStatusRunning')
      : step.status === 'failed'
        ? step.result?.uiMeta?.approvalStatus === 'rejected'
          ? t('toolApprovalStatusRejected')
          : t('toolStatusFailed')
        : typeof exitCode === 'number' && exitCode !== 0
          ? t('toolExitCode', { code: exitCode })
          : t('toolStatusDone')

  const inputPreview = step.use.parseError
    ? step.use.partialJson || step.use.parseError
    : JSON.stringify(step.use.input, null, 2)

  return (
    <div className={clsx(styles.wrapper, !isLast && styles.withLine)}>
      <span className={styles.statusDot}>
        <span
          className={clsx(
            styles.dot,
            (step.status === 'wip' || pendingApproval) && styles.dotRunning,
            !pendingApproval && step.status === 'done' && styles.dotDone,
            !pendingApproval && step.status === 'failed' && styles.dotError
          )}
        />
      </span>
      <div className={styles.card}>
        <button
          type="button"
          className={styles.header}
          onClick={() => setExpanded((value) => !value)}
        >
          <span className={styles.toolName}>{step.use.name}</span>
          {summary ? (
            <span className={styles.summary} title={summary}>
              {summary}
            </span>
          ) : null}
          <span
            className={clsx(
              styles.statusText,
              (step.status === 'failed' || (typeof exitCode === 'number' && exitCode !== 0)) &&
                styles.statusError
            )}
          >
            {statusLabel}
          </span>
          <span className={styles.chevron}>
            {expanded ? <IconChevronDownOutline14 /> : <IconChevronRightOutline14 />}
          </span>
        </button>

        {expanded ? (
          <div className={styles.body}>
            <div className={styles.sectionLabel}>{t('toolInputLabel')}</div>
            <pre className={styles.pre}>{inputPreview}</pre>

            {diffs.map((diff, index) => (
              <DiffView key={`${diff.path}-${index}`} diff={diff} />
            ))}

            {producedFiles.length > 0 ? (
              <div className={styles.fileActions}>
                {producedFiles.map((filePath) => (
                  <div key={filePath} className={styles.fileChip} title={filePath}>
                    <span className={styles.fileChipName}>{basenameOfPath(filePath)}</span>
                    <button
                      type="button"
                      className={styles.fileChipBtn}
                      onClick={() => {
                        void openWorkspacePath(filePath)
                      }}
                    >
                      {t('toolOpenFile')}
                    </button>
                    <button
                      type="button"
                      className={styles.fileChipBtn}
                      onClick={() => {
                        void revealWorkspaceInFinder(filePath)
                      }}
                    >
                      {t('workspaceRevealInFinder')}
                    </button>
                  </div>
                ))}
              </div>
            ) : null}

            {step.result ? (
              <>
                <div className={styles.sectionLabel}>{t('toolOutputLabel')}</div>
                <pre className={clsx(styles.pre, step.result.isError && styles.preError)}>
                  {step.result.content}
                </pre>
              </>
            ) : null}
          </div>
        ) : null}
      </div>
      {pendingApproval ? <ApprovalCard approval={pendingApproval} /> : null}
    </div>
  )
})
