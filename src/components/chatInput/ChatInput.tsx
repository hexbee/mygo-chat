import clsx from 'clsx'
import { memo, useCallback, useEffect, useLayoutEffect, useRef } from 'react'
import {
  IconGlobeLanguageOutline14,
  IconPaperclipOutline16,
  IconSendOutline16,
  IconStopFill16,
  IconThinkOutline14,
} from '../base/icons.js'
import { setSearch, setThinking, useSettings } from '../../services/settings.js'
import { useIsMobile } from '../../app/viewport.js'
import { sendMessage, stopReply, useChatStore } from '../../services/chat/store.js'
import {
  addDraftFiles,
  consumePendingQuote,
  EMPTY_DRAFT,
  getDraft,
  prepareDraftMessage,
  removeDraftAttachment,
  retryDraftAttachment,
  setDraftSegments,
  setDraftText,
  useDrafts,
  type DraftSegment,
} from '../../services/chat/drafts.js'
import { createId } from '../../utils/id.js'
import { stopReadAloud } from '../../services/speech/readAloud.js'
import { IconButton } from '../base/IconButton.js'
import { Tooltip } from '../base/Tooltip.js'
import { ensureApiKey } from '../settings/settingsDialogState.js'
import { DragMask, useFileDrop } from './DragMask.js'
import { showToast } from '../base/toastState.js'
import { ACCEPT, useUploads } from '../../services/attachments.js'
import { DraftAttachments } from './DraftAttachments.js'
import { useVoiceInput } from './useVoiceInput.js'
import { VoiceButton } from './VoiceButton.js'
import { Spectrum } from './Spectrum.js'
import {
  handleAtomicQuoteDeletion,
  insertPlainTextAtRange,
  insertQuoteAtRange,
  isCaretAtEnd,
  isCaretAtStart,
  moveCaretToEnd,
  parseEditorSegments,
  removeQuoteToken,
  renderEditorSegments,
} from './quoteEditor.js'
import { useTranslate } from '../../i18n/index.js'
import styles from './ChatInput.module.css'

const QUOTE_CLASSES = {
  token: styles.quoteToken,
  icon: styles.quoteIcon,
  quoteGlyph: styles.quoteGlyph,
  removeGlyph: styles.removeGlyph,
  label: styles.quoteLabel,
}

/** 输入框：自适应高度的富文本输入框（支持内联多引用） + 文件 + 深度思考与智能搜索开关 + 语音输入 + 发送 / 停止 */
export const ChatInput = memo(function ChatInput({ sessionId }: { sessionId: string | null }) {
  // 切换目标时释放录音、转写和 DOM 状态；文字与附件由草稿服务保留。
  return <Input key={sessionId === null ? 'new' : `session:${sessionId}`} sessionId={sessionId} />
})

const Input = ({ sessionId }: { sessionId: string | null }) => {
  const t = useTranslate()
  const draft = useDrafts((state) => state.drafts.get(sessionId) ?? EMPTY_DRAFT)
  const pendingQuote = useDrafts((state) => state.pendingQuote)
  const { segments, attachments } = draft
  const isEmpty = segments.length === 0
  const draftReady = useUploads((state) => prepareDraftMessage(draft, state.tasks) !== null)
  const streaming = useChatStore(
    (state) => sessionId !== null && Object.hasOwn(state.streaming, sessionId)
  )
  const thinking = useSettings((state) => state.thinking)
  const importingHistory = useChatStore((state) => state.importingHistory)
  const isMobile = useIsMobile()
  const search = useSettings((state) => state.search)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const editorRef = useRef<HTMLDivElement>(null)
  const togglesRef = useRef<HTMLDivElement>(null)
  const historyIndexRef = useRef<number>(-1)
  const savedDraftRef = useRef<DraftSegment[]>([])
  const savedRangeRef = useRef<Range | null>(null)
  const composingRef = useRef(false)
  const lastSyncedSegmentsRef = useRef<DraftSegment[] | null>(null)

  const syncFromDom = useCallback(() => {
    const editor = editorRef.current
    if (!editor) {
      return
    }
    const nextSegments = parseEditorSegments(editor)
    lastSyncedSegmentsRef.current = nextSegments
    setDraftSegments(sessionId, nextSegments)
  }, [sessionId])

  const getUserMessagesHistory = useCallback((): DraftSegment[][] => {
    if (!sessionId) {
      return []
    }
    const session = useChatStore.getState().sessionCache[sessionId]
    if (!session || !session.messages) {
      return []
    }
    const userMsgs: DraftSegment[][] = []
    for (let i = session.messages.length - 1; i >= 0; i--) {
      const m = session.messages[i]
      if (m.role === 'user' && (m.content?.trim() || m.segments?.length)) {
        if (m.segments && m.segments.length > 0) {
          userMsgs.push(
            m.segments.map((segment) =>
              segment.type === 'quote'
                ? { type: 'quote', id: createId(), text: segment.text }
                : { type: 'text', text: segment.text }
            )
          )
        } else {
          userMsgs.push([{ type: 'text', text: m.content }])
        }
      }
    }
    return userMsgs
  }, [sessionId])

  // 外部草稿变化（初始化、清空、切换历史等）同步到 contentEditable DOM；本地输入已同步时跳过，避免打断光标与 IME。
  useLayoutEffect(() => {
    const editor = editorRef.current
    if (!editor || lastSyncedSegmentsRef.current === segments) {
      return
    }
    renderEditorSegments(editor, segments, QUOTE_CLASSES)
    lastSyncedSegmentsRef.current = segments
  }, [segments])

  // 桌面端挂载后自动聚焦输入框
  useEffect(() => {
    const editor = editorRef.current
    if (!isMobile && editor && !importingHistory) {
      editor.focus({ preventScroll: true })
      moveCaretToEnd(editor)
    }
  }, [isMobile, importingHistory])

  // 记录输入框内最近一次光标位置，便于选中消息文本后点击「引用文本」时在原光标处插入
  useEffect(() => {
    const onSelectionChange = () => {
      const editor = editorRef.current
      const selection = window.getSelection()
      if (!editor || !selection || selection.rangeCount === 0) {
        return
      }
      const range = selection.getRangeAt(0)
      if (editor.contains(range.startContainer) && editor.contains(range.endContainer)) {
        savedRangeRef.current = range.cloneRange()
      }
    }
    document.addEventListener('selectionchange', onSelectionChange)
    return () => document.removeEventListener('selectionchange', onSelectionChange)
  }, [])

  // 响应消息列表发来的引用文本插入请求，支持连续多引用
  useEffect(() => {
    if (!pendingQuote || pendingQuote.sessionId !== sessionId) {
      return
    }
    const editor = editorRef.current
    if (!editor) {
      return
    }
    consumePendingQuote(pendingQuote.seq)
    historyIndexRef.current = -1
    savedRangeRef.current = insertQuoteAtRange(
      editor,
      savedRangeRef.current,
      createId(),
      pendingQuote.text,
      QUOTE_CLASSES
    )
    syncFromDom()
  }, [pendingQuote, sessionId, syncFromDom])

  useLayoutEffect(() => {
    const toggles = togglesRef.current
    if (!toggles) {
      return
    }

    const updateLabels = () => {
      toggles.classList.remove(styles.collapsedToggles)
      toggles.classList.toggle(styles.collapsedToggles, toggles.scrollWidth > toggles.clientWidth)
    }

    updateLabels()
    const observer = new ResizeObserver(updateLabels)
    observer.observe(toggles)
    return () => observer.disconnect()
  }, [t])

  const acceptFiles = (files: File[]) => {
    if (files.length === 0 || useChatStore.getState().importingHistory || !ensureApiKey()) {
      return
    }
    const error = addDraftFiles(sessionId, files, useSettings.getState().apiKey ?? '')
    if (error) {
      showToast(error)
    }
  }
  const retryFile = useCallback(
    (id: string) => {
      if (!useChatStore.getState().importingHistory && ensureApiKey()) {
        retryDraftAttachment(sessionId, id, useSettings.getState().apiKey ?? '')
      }
    },
    [sessionId]
  )
  const removeFile = useCallback(
    (id: string) => {
      if (!useChatStore.getState().importingHistory) {
        removeDraftAttachment(sessionId, id)
      }
    },
    [sessionId]
  )
  const dragging = useFileDrop(acceptFiles)

  /** 转写结果插入到光标处，并把光标移到插入内容之后 */
  const voice = useVoiceInput((transcript) => {
    const editor = editorRef.current
    if (!editor) {
      const current = getDraft(sessionId).text
      setDraftText(sessionId, current + transcript)
      return
    }
    historyIndexRef.current = -1
    savedRangeRef.current = insertPlainTextAtRange(editor, savedRangeRef.current, transcript)
    syncFromDom()
  })

  // 欢迎页导入后会话 ID 不变，不能依赖卸载取消语音；订阅开始事件也覆盖快速完成的导入。
  const cancelVoice = voice.cancel
  useEffect(
    () =>
      useChatStore.subscribe((state, previous) => {
        if (state.importingHistory && !previous.importingHistory) {
          cancelVoice()
        }
      }),
    [cancelVoice]
  )

  // 移动端录音时用频谱替代输入框内容
  const recording = voice.status === 'recording'

  // 排队、上传中或上传失败的附件都留在输入框，全部上传成功后才能发送
  const canSend = !importingHistory && !streaming && voice.status === 'idle' && draftReady

  const send = () => {
    if (
      useChatStore.getState().importingHistory ||
      voice.status !== 'idle' ||
      !prepareDraftMessage(getDraft(sessionId), useUploads.getState().tasks)
    ) {
      return
    }
    // 文本与上传完成态由提交动作读取最新快照，不使用上一次渲染的草稿。
    if (!ensureApiKey() || !sendMessage(sessionId)) {
      return
    }
    savedRangeRef.current = null
    stopReadAloud()
    if (isMobile) {
      editorRef.current?.blur()
    } else {
      editorRef.current?.focus({ preventScroll: true })
    }
  }

  return (
    <div
      className={clsx(styles.wrapper, sessionId === null ? styles.home : styles.session)}
      onClick={(event) => {
        if (event.target === event.currentTarget && editorRef.current) {
          editorRef.current.focus({ preventScroll: true })
          moveCaretToEnd(editorRef.current)
        }
      }}
    >
      <DraftAttachments items={attachments} onRetry={retryFile} onRemove={removeFile} />
      <div className={styles.textareaWrapper}>
        <div
          ref={editorRef}
          role="textbox"
          aria-multiline="true"
          aria-label={t('chatInputPlaceholderChat')}
          aria-disabled={importingHistory || undefined}
          contentEditable={!importingHistory}
          suppressContentEditableWarning
          data-placeholder={t('chatInputPlaceholderChat')}
          data-empty={isEmpty ? 'true' : undefined}
          className={clsx(styles.editor, recording && isMobile && styles.textareaHidden)}
          onCompositionStart={() => {
            composingRef.current = true
          }}
          onCompositionEnd={() => {
            composingRef.current = false
            historyIndexRef.current = -1
            syncFromDom()
          }}
          onInput={() => {
            if (composingRef.current) {
              return
            }
            historyIndexRef.current = -1
            syncFromDom()
          }}
          onMouseDown={(event) => {
            const removeButton = (event.target as Element | null)?.closest(
              '[data-quote-remove="true"]'
            )
            if (removeButton && editorRef.current) {
              const token = removeButton.closest<HTMLElement>('[data-quote-id]')
              if (token) {
                event.preventDefault()
                historyIndexRef.current = -1
                savedRangeRef.current = removeQuoteToken(editorRef.current, token)
                syncFromDom()
                return
              }
            }
            // 首次点击时手动聚焦，避免 Safari 把整个页面滚到输入框位置。
            if (isMobile && event.button === 0 && document.activeElement !== event.currentTarget) {
              event.preventDefault()
              event.currentTarget.focus({ preventScroll: true })
            }
          }}
          onKeyDown={(event) => {
            if (event.nativeEvent.isComposing || composingRef.current) {
              return
            }

            if (event.key === 'Backspace' && editorRef.current) {
              if (handleAtomicQuoteDeletion(editorRef.current, 'backward')) {
                event.preventDefault()
                historyIndexRef.current = -1
                syncFromDom()
                return
              }
            }

            if (event.key === 'Delete' && editorRef.current) {
              if (handleAtomicQuoteDeletion(editorRef.current, 'forward')) {
                event.preventDefault()
                historyIndexRef.current = -1
                syncFromDom()
                return
              }
            }

            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              historyIndexRef.current = -1
              send()
              return
            }

            // 按方向键 ↑ 快速调出上一条发送的消息进行重新编辑或重发
            if (
              event.key === 'ArrowUp' &&
              !event.shiftKey &&
              !event.altKey &&
              !event.metaKey &&
              !event.ctrlKey &&
              editorRef.current
            ) {
              if (isEmpty || isCaretAtStart(editorRef.current)) {
                const history = getUserMessagesHistory()
                if (history.length > 0) {
                  const nextIndex = historyIndexRef.current + 1
                  if (nextIndex < history.length) {
                    event.preventDefault()
                    if (historyIndexRef.current === -1) {
                      savedDraftRef.current = segments
                    }
                    historyIndexRef.current = nextIndex
                    setDraftSegments(sessionId, history[nextIndex])
                    requestAnimationFrame(() => {
                      if (editorRef.current) {
                        moveCaretToEnd(editorRef.current)
                      }
                    })
                  }
                }
              }
              return
            }

            // 按方向键 ↓ 切换回更新的消息或之前未发送的草稿
            if (
              event.key === 'ArrowDown' &&
              !event.shiftKey &&
              !event.altKey &&
              !event.metaKey &&
              !event.ctrlKey &&
              editorRef.current
            ) {
              if ((isEmpty || isCaretAtEnd(editorRef.current)) && historyIndexRef.current >= 0) {
                event.preventDefault()
                const history = getUserMessagesHistory()
                const nextIndex = historyIndexRef.current - 1
                historyIndexRef.current = nextIndex
                setDraftSegments(
                  sessionId,
                  nextIndex === -1 ? savedDraftRef.current : history[nextIndex]
                )
                requestAnimationFrame(() => {
                  if (editorRef.current) {
                    moveCaretToEnd(editorRef.current)
                  }
                })
              }
              return
            }
          }}
          onPaste={(event) => {
            const files = Array.from(event.clipboardData.files)
            if (files.length > 0) {
              event.preventDefault()
              acceptFiles(files)
              return
            }
            const plainText = event.clipboardData.getData('text/plain')
            event.preventDefault()
            if (plainText && editorRef.current) {
              historyIndexRef.current = -1
              savedRangeRef.current = insertPlainTextAtRange(
                editorRef.current,
                savedRangeRef.current,
                plainText
              )
              syncFromDom()
            }
          }}
        />
        {recording && isMobile ? <Spectrum getAnalyser={voice.getAnalyser} /> : null}
      </div>
      <div className={styles.functionRow}>
        {/* 深度思考 / 智能搜索开关，tooltip 在按钮上方 */}
        <div ref={togglesRef} className={styles.toggles}>
          <Tooltip content={t('deactivatedThinkTooltip')}>
            <button
              type="button"
              className={clsx(styles.toggle, thinking && styles.selected)}
              aria-label={t('thinkingSwitch')}
              aria-pressed={thinking}
              onClick={() => setThinking(!thinking)}
            >
              <IconThinkOutline14 />
              <span className={styles.toggleLabel}>{t('thinkingSwitch')}</span>
            </button>
          </Tooltip>
          <Tooltip content={t('deactivatedSearchTooltip')}>
            <button
              type="button"
              className={clsx(styles.toggle, search && styles.selected)}
              aria-label={t('search')}
              aria-pressed={search}
              onClick={() => setSearch(!search)}
            >
              <IconGlobeLanguageOutline14 />
              <span className={styles.toggleLabel}>{t('search')}</span>
            </button>
          </Tooltip>
        </div>
        <div className={styles.right}>
          <VoiceButton
            status={voice.status}
            disabled={!voice.available}
            seconds={voice.seconds}
            onClick={() => {
              if (
                voice.status !== 'idle' ||
                (!useChatStore.getState().importingHistory && ensureApiKey())
              ) {
                void voice.toggle()
              }
            }}
          />
          <IconButton
            size="m"
            tone="primary"
            icon={<IconPaperclipOutline16 />}
            tooltip={t('uploadFile')}
            disabled={importingHistory}
            onClick={() => ensureApiKey() && fileInputRef.current?.click()}
          />
          <input
            ref={fileInputRef}
            type="file"
            multiple
            hidden
            accept={ACCEPT}
            onChange={(event) => {
              acceptFiles(Array.from(event.target.files ?? []))
              event.target.value = ''
            }}
          />
          {streaming && sessionId ? (
            <button
              type="button"
              className={styles.sendButton}
              aria-label={t('chatInputStopButtonTooltip')}
              onClick={() => {
                stopReadAloud()
                stopReply(sessionId)
              }}
            >
              <IconStopFill16 />
            </button>
          ) : (
            <button
              type="button"
              className={clsx(styles.sendButton, !canSend && styles.disabled)}
              aria-label={t('send')}
              aria-disabled={!canSend}
              onClick={send}
            >
              <IconSendOutline16 />
            </button>
          )}
        </div>
      </div>
      {dragging ? <DragMask /> : null}
    </div>
  )
}
