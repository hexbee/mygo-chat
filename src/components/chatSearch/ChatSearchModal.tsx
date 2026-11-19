import clsx from 'clsx'
import { useEffect, useRef, useState } from 'react'
import { useTranslate } from '../../i18n/index.js'
import {
  formatSearchDate,
  searchChats,
  splitHighlight,
  type ChatSearchHit,
} from '../../services/chat/search.js'
import { selectSession } from '../../services/chat/store.js'
import {
  IconChatBubbleOutline16,
  IconCloseOutline16,
  IconSearchOutline16,
  IconUserOutline10,
} from '../base/icons.js'
import { closeMobileSider } from '../sider/mobileSiderState.js'
import {
  closeChatSearch,
  requestScrollToMessage,
  useChatSearch,
} from './chatSearchState.js'
import styles from './ChatSearchModal.module.css'

const HighlightedText = ({ text, query }: { text: string; query: string }) => (
  <>
    {splitHighlight(text, query).map((seg, index) =>
      seg.match ? (
        <span key={index} className={styles.highlight}>
          {seg.text}
        </span>
      ) : (
        <span key={index}>{seg.text}</span>
      )
    )}
  </>
)

export const ChatSearchModal = () => {
  const open = useChatSearch((state) => state.open)
  return open ? <ChatSearchContent /> : null
}

const ChatSearchContent = () => {
  const t = useTranslate()
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<ChatSearchHit[]>([])
  const [searching, setSearching] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const trimmed = query.trim()
  const hasQuery = trimmed.length > 0

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    if (!hasQuery) {
      setHits([])
      setSearching(false)
      setActiveIndex(0)
      return
    }
    let cancelled = false
    setSearching(true)
    const timer = setTimeout(() => {
      void searchChats(trimmed)
        .then((results) => {
          if (!cancelled) {
            setHits(results)
            setActiveIndex(0)
          }
        })
        .catch(() => {
          if (!cancelled) {
            setHits([])
          }
        })
        .finally(() => {
          if (!cancelled) {
            setSearching(false)
          }
        })
    }, 90)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [hasQuery, trimmed])

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-hit-index="${activeIndex}"]`)
    el?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex])

  const openHit = (hit: ChatSearchHit) => {
    if (hit.messageId) {
      requestScrollToMessage(hit.sessionId, hit.messageId)
    }
    selectSession(hit.sessionId)
    closeMobileSider()
    closeChatSearch()
  }

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.nativeEvent.isComposing) {
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      closeChatSearch()
      return
    }
    if (hits.length === 0) {
      return
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActiveIndex((prev) => (prev + 1) % hits.length)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActiveIndex((prev) => (prev - 1 + hits.length) % hits.length)
    } else if (event.key === 'Enter') {
      event.preventDefault()
      const target = hits[activeIndex]
      if (target) {
        openHit(target)
      }
    }
  }

  return (
    <div
      className={styles.backdrop}
      role="dialog"
      aria-modal="true"
      aria-label={t('searchChats')}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          closeChatSearch()
        }
      }}
      onKeyDown={onKeyDown}
    >
      <div className={clsx(styles.panel, hasQuery && styles.panelExpanded)}>
        <div className={styles.inputRow}>
          <span className={styles.searchIcon} aria-hidden="true">
            <IconSearchOutline16 />
          </span>
          <input
            ref={inputRef}
            type="text"
            className={styles.input}
            value={query}
            placeholder={t('searchChatsPlaceholder')}
            aria-label={t('searchChatsPlaceholder')}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => setQuery(event.target.value)}
          />
          <span className={styles.divider} aria-hidden="true" />
          <button
            type="button"
            className={styles.closeButton}
            aria-label={t('close')}
            onClick={closeChatSearch}
          >
            <IconCloseOutline16 />
          </button>
        </div>
        {hasQuery ? (
          <div className={styles.resultsWrap}>
            {hits.length > 0 ? (
              <div ref={listRef} className={styles.resultsList} role="listbox">
                {hits.map((hit, index) => (
                  <button
                    key={hit.id}
                    type="button"
                    role="option"
                    aria-selected={index === activeIndex}
                    data-hit-index={index}
                    className={clsx(
                      styles.resultItem,
                      index === activeIndex && styles.resultItemActive
                    )}
                    onMouseEnter={() => setActiveIndex(index)}
                    onClick={() => openHit(hit)}
                  >
                    <span className={styles.avatar} aria-hidden="true">
                      <IconChatBubbleOutline16 />
                      {hit.kind === 'user' ? (
                        <span className={styles.userBadge}>
                          <IconUserOutline10 />
                        </span>
                      ) : null}
                    </span>
                    <span className={styles.itemBody}>
                      <span className={styles.itemHeader}>
                        <span className={styles.itemTitle}>
                          <HighlightedText
                            text={hit.sessionTitle || t('untitledChat')}
                            query={trimmed}
                          />
                        </span>
                        <span className={styles.itemDate}>{formatSearchDate(hit.timestamp)}</span>
                      </span>
                      <span className={styles.itemSnippet}>
                        <HighlightedText text={hit.snippet} query={trimmed} />
                      </span>
                    </span>
                  </button>
                ))}
              </div>
            ) : !searching ? (
              <div className={styles.empty}>{t('searchChatsNoResult')}</div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
}
