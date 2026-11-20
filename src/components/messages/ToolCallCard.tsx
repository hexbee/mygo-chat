import clsx from 'clsx'
import { memo, useState } from 'react'
import { useTranslate } from '../../i18n/index.js'
import type { ClientToolStep } from '../../services/chat/view.js'
import {
  basenameOfPath,
  openWorkspacePath,
  revealWorkspaceInFinder,
} from '../../services/workspace/store.js'
import { IconChevronDownOutline14, IconChevronRightOutline14 } from '../base/icons.js'
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
  const hasDiffs = Boolean(step.result?.uiMeta?.diffs?.length)
  const [expanded, setExpanded] = useState(hasDiffs)
  const summary = summarizeToolCall(step.use.name, step.use.input)
  const diffs = step.result?.uiMeta?.diffs ?? []
  const producedFiles = step.result?.uiMeta?.producedFiles ?? []

  const statusLabel =
    step.status === 'wip'
      ? t('toolStatusRunning')
      : step.status === 'failed'
        ? t('toolStatusFailed')
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
            step.status === 'wip' && styles.dotRunning,
            step.status === 'done' && styles.dotDone,
            step.status === 'failed' && styles.dotError
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
          <span className={clsx(styles.statusText, step.status === 'failed' && styles.statusError)}>
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
    </div>
  )
})
