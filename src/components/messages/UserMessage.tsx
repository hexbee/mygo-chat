import { useTranslate } from '../../i18n/index.js'
import clsx from 'clsx'
import { memo, useLayoutEffect, useRef, useState } from 'react'
import { useIsMobile } from '../../app/viewport.js'
import { IconEditOutline16 } from '../base/icons.js'
import { editMessage, retryUpload } from '../../services/chat/store.js'
import { stopReadAloud } from '../../services/speech/readAloud.js'
import type { UserMessage as UserMessageType } from '../../services/chat/types.js'
import { Button } from '../base/Button.js'
import { IconButton } from '../base/IconButton.js'
import { FileMentionIcon, FolderMentionIcon } from '../chatInput/AtFileMenu.js'
import { formatQuoteDisplay } from '../chatInput/quoteEditor.js'
import { ensureApiKey } from '../settings/settingsDialogState.js'
import {
  BranchSwitcher,
  type MessageVersions,
  type SelectMessageVersion,
} from './BranchSwitcher.js'
import { MessageFile } from './MessageFile.js'
import { CopyButton } from './CopyButton.js'
import styles from './UserMessage.module.css'

const QuoteMarkIcon = () => (
  <svg width={13} height={13} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path
      d="M6.8 3.2C4.15 4.42 2.5 6.72 2.5 9.55C2.5 11.42 3.74 12.7 5.38 12.7C6.82 12.7 7.9 11.63 7.9 10.22C7.9 8.88 6.92 7.88 5.66 7.82C5.45 7.81 5.24 7.83 5.04 7.89C5.29 6.38 6.24 5.17 7.58 4.44L6.8 3.2ZM13.5 3.2C10.85 4.42 9.2 6.72 9.2 9.55C9.2 11.42 10.44 12.7 12.08 12.7C13.52 12.7 14.6 11.63 14.6 10.22C14.6 8.88 13.62 7.88 12.36 7.82C12.15 7.81 11.94 7.83 11.74 7.89C11.99 6.38 12.94 5.17 14.28 4.44L13.5 3.2Z"
      fill="currentColor"
    />
  </svg>
)

const SkillBadgeIcon = () => (
  <svg width={15} height={15} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path
      d="M6.8 1.8L8.05 5.45L11.7 6.7L8.05 7.95L6.8 11.6L5.55 7.95L1.9 6.7L5.55 5.45L6.8 1.8Z"
      stroke="currentColor"
      strokeWidth={1.3}
      strokeLinejoin="round"
    />
    <path
      d="M12.3 9.7L12.9 11.4L14.6 12L12.9 12.6L12.3 14.3L11.7 12.6L10 12L11.7 11.4L12.3 9.7Z"
      fill="currentColor"
    />
  </svg>
)

const CommandBadgeIcon = () => (
  <svg width={15} height={15} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <rect x={2} y={2} width={12} height={12} rx={3.5} stroke="currentColor" strokeWidth={1.3} />
    <path d="M9.6 4.7L6.4 11.3" stroke="currentColor" strokeWidth={1.3} strokeLinecap="round" />
  </svg>
)

export const UserMessage = memo(function UserMessage({
  sessionId,
  message,
  retryUploadId,
  isFirst,
  isLast,
  busy,
  versions,
  onSelectVersion,
}: {
  sessionId: string
  message: UserMessageType
  /** 回答因补传附件失败可重试时为该回答 id */
  retryUploadId: string | null
  isFirst: boolean
  isLast: boolean
  busy: boolean
  versions: MessageVersions
  onSelectVersion: SelectMessageVersion
}) {
  const t = useTranslate()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(message.content)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const isMobile = useIsMobile()
  const canSend = !busy && (draft.trim() !== '' || message.files.length > 0)

  useLayoutEffect(() => {
    const textarea = textareaRef.current
    if (editing && textarea) {
      textarea.style.height = 'auto'
      textarea.style.height = `${Math.min(textarea.scrollHeight, 202)}px`
    }
  }, [editing, draft])

  const sendEdit = () => {
    if (canSend && ensureApiKey() && editMessage(sessionId, message.id, draft.trim())) {
      stopReadAloud()
      setEditing(false)
    }
  }

  return (
    <div data-message-id={message.id} className={clsx(styles.container, isFirst && styles.isFirst)}>
      {message.files.length > 0 ? (
        <div
          className={clsx(
            styles.files,
            message.files.some((file) => !file.isImage) && styles.filesWithDocuments
          )}
        >
          {message.files.map((file) => (
            <MessageFile
              key={file.id}
              file={file}
              onRetry={retryUploadId ? () => retryUpload(sessionId, retryUploadId) : null}
            />
          ))}
        </div>
      ) : null}
      {editing ? (
        <div className={styles.editor}>
          <textarea
            ref={textareaRef}
            className={styles.textarea}
            aria-label={t('editMessage')}
            autoFocus
            rows={1}
            value={draft}
            disabled={busy}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) {
                return
              }
              if (event.key === 'Escape') {
                setEditing(false)
              } else if (event.key === 'Enter' && !event.shiftKey && !event.ctrlKey && !isMobile) {
                event.preventDefault()
                sendEdit()
              }
            }}
          />
          <div className={styles.editorActions}>
            <Button size="s" variant="outlined" onClick={() => setEditing(false)}>
              {t('cancel')}
            </Button>
            <Button size="s" disabled={!canSend} onClick={sendEdit}>
              {t('send')}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {message.content || message.segments?.length ? (
            <div className={styles.bubble}>
              {message.segments && message.segments.length > 0
                ? message.segments.map((segment, index) =>
                    segment.type === 'quote' ? (
                      <span
                        key={index}
                        className={clsx(styles.quoteToken, index > 0 && styles.quoteTokenSpaced)}
                        data-quote-preview="true"
                        data-quote-text={segment.text}
                      >
                        <span className={styles.quoteIcon} aria-hidden="true">
                          <QuoteMarkIcon />
                        </span>
                        <span className={styles.quoteLabel}>
                          {formatQuoteDisplay(segment.text)}
                        </span>
                      </span>
                    ) : segment.type === 'skill' ? (
                      <span key={index} className={styles.skillToken}>
                        <span className={styles.skillIcon} aria-hidden="true">
                          {segment.kind === 'command' ? <CommandBadgeIcon /> : <SkillBadgeIcon />}
                        </span>
                        <span className={styles.skillLabel}>{segment.name}</span>
                      </span>
                    ) : segment.type === 'file' ? (
                      <span key={index} className={styles.fileToken} title={segment.path}>
                        <span className={styles.fileIcon} aria-hidden="true">
                          {segment.kind === 'directory' ? (
                            <FolderMentionIcon />
                          ) : (
                            <FileMentionIcon />
                          )}
                        </span>
                        <span className={styles.fileLabel}>
                          {segment.kind === 'directory' ? `${segment.name}/` : segment.name}
                        </span>
                      </span>
                    ) : (
                      <span key={index}>{segment.text}</span>
                    )
                  )
                : message.content}
            </div>
          ) : null}
          <div className={styles.bottom}>
            <div className={clsx(styles.actions, isLast && styles.alwaysShow)}>
              {message.content ? <CopyButton text={message.content} /> : null}
              <IconButton
                icon={<IconEditOutline16 />}
                tooltip={t('editMessage')}
                tooltipPlacement="bottom"
                disabled={busy}
                onClick={() => {
                  setDraft(message.content)
                  setEditing(true)
                }}
              />
            </div>
            <BranchSwitcher versions={versions} disabled={busy} onSelect={onSelectVersion} />
          </div>
        </>
      )}
    </div>
  )
})
