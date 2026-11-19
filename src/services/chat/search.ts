import { getVisibleMessages } from './branches.js'
import * as db from './db.js'
import { useChatStore } from './store.js'
import type { AssistantMessage, Session } from './types.js'

export type ChatSearchMatchKind = 'title' | 'user' | 'assistant'

export type ChatSearchHit = {
  id: string
  sessionId: string
  sessionTitle: string
  messageId: string | null
  kind: ChatSearchMatchKind
  snippet: string
  timestamp: number
}

export type HighlightSegment = {
  text: string
  match: boolean
}

const MAX_HITS_PER_SESSION = 5
const MAX_TOTAL_HITS = 100
const SNIPPET_BEFORE = 22
const SNIPPET_AFTER = 120

const normalizeWhitespace = (text: string): string => text.replace(/\s+/g, ' ').trim()

const extractAssistantText = (message: AssistantMessage): string => {
  if (message.stopReason === 'refusal') {
    return ''
  }
  const parts: string[] = []
  for (const block of message.blocks) {
    if (block.type === 'text' && !block.isTemplate && block.text) {
      parts.push(block.text)
    }
  }
  return parts.join(' ')
}

/** 围绕首个命中词截取上下文摘要，首尾被截断时自动补省略号 */
const buildSnippet = (rawText: string, queryLower: string): string => {
  const text = normalizeWhitespace(rawText)
  if (!text) {
    return ''
  }
  const lower = text.toLowerCase()
  const idx = lower.indexOf(queryLower)
  if (idx === -1) {
    return text.length > SNIPPET_AFTER ? `${text.slice(0, SNIPPET_AFTER)}...` : text
  }
  const start = Math.max(0, idx - SNIPPET_BEFORE)
  const end = Math.min(text.length, idx + queryLower.length + SNIPPET_AFTER)
  const prefix = start > 0 ? '...' : ''
  const suffix = end < text.length ? '...' : ''
  return `${prefix}${text.slice(start, end).trim()}${suffix}`
}

/** 将文本按关键词大小写不敏感切分为普通与高亮片段 */
export const splitHighlight = (text: string, query: string): HighlightSegment[] => {
  const trimmed = query.trim()
  if (!trimmed || !text) {
    return [{ text, match: false }]
  }
  const lowerText = text.toLowerCase()
  const lowerQuery = trimmed.toLowerCase()
  const segments: HighlightSegment[] = []
  let cursor = 0
  while (cursor < text.length) {
    const idx = lowerText.indexOf(lowerQuery, cursor)
    if (idx === -1) {
      segments.push({ text: text.slice(cursor), match: false })
      break
    }
    if (idx > cursor) {
      segments.push({ text: text.slice(cursor, idx), match: false })
    }
    segments.push({ text: text.slice(idx, idx + trimmed.length), match: true })
    cursor = idx + trimmed.length
  }
  return segments.length > 0 ? segments : [{ text, match: false }]
}

/** 日期格式化：同年显示 M/D（如 11/8），跨年显示 M/D/YYYY（如 9/23/2025） */
export const formatSearchDate = (timestamp: number, now = Date.now()): string => {
  const date = new Date(timestamp)
  const current = new Date(now)
  const month = date.getMonth() + 1
  const day = date.getDate()
  const year = date.getFullYear()
  if (year === current.getFullYear()) {
    return `${month}/${day}`
  }
  return `${month}/${day}/${year}`
}

/**
 * 在所有本地会话（IndexedDB + 内存最新缓存）中搜索标题与消息正文。
 * 返回按时间倒序排列的命中项，区分用户消息与助手消息。
 */
export const searchChats = async (query: string): Promise<ChatSearchHit[]> => {
  const trimmed = query.trim()
  if (!trimmed) {
    return []
  }
  const queryLower = trimmed.toLowerCase()

  const stored = await db.loadAllSessions()
  const state = useChatStore.getState()
  const sessionMap = new Map<string, Session>()
  for (const session of stored) {
    sessionMap.set(session.id, session)
  }
  // 内存中的缓存可能包含尚未落盘或正在流式生成的最新消息，优先使用
  for (const [id, cached] of Object.entries(state.sessionCache)) {
    sessionMap.set(id, cached)
  }

  const hits: ChatSearchHit[] = []

  for (const session of sessionMap.values()) {
    const messages = getVisibleMessages(session)
    let sessionMessageHits = 0

    // 从最新消息往前遍历，优先保留最近命中的消息
    for (let i = messages.length - 1; i >= 0; i -= 1) {
      if (sessionMessageHits >= MAX_HITS_PER_SESSION) {
        break
      }
      const message = messages[i]
      if (message.role === 'user') {
        const text = normalizeWhitespace(message.content)
        if (text && text.toLowerCase().includes(queryLower)) {
          hits.push({
            id: `${session.id}:${message.id}`,
            sessionId: session.id,
            sessionTitle: session.title,
            messageId: message.id,
            kind: 'user',
            snippet: buildSnippet(text, queryLower),
            timestamp: message.createdAt || session.updatedAt,
          })
          sessionMessageHits += 1
        }
      } else {
        const text = normalizeWhitespace(extractAssistantText(message))
        if (text && text.toLowerCase().includes(queryLower)) {
          hits.push({
            id: `${session.id}:${message.id}`,
            sessionId: session.id,
            sessionTitle: session.title,
            messageId: message.id,
            kind: 'assistant',
            snippet: buildSnippet(text, queryLower),
            timestamp: message.createdAt || session.updatedAt,
          })
          sessionMessageHits += 1
        }
      }
    }

    // 若消息正文均未命中但会话标题命中，仍返回一条标题命中项
    if (
      sessionMessageHits === 0 &&
      session.title &&
      session.title.toLowerCase().includes(queryLower)
    ) {
      const firstText = messages.find((m) => m.role === 'user')?.content ?? ''
      hits.push({
        id: `${session.id}:title`,
        sessionId: session.id,
        sessionTitle: session.title,
        messageId: null,
        kind: 'title',
        snippet: normalizeWhitespace(firstText) || session.title,
        timestamp: session.updatedAt || session.createdAt,
      })
    }
  }

  hits.sort((a, b) => b.timestamp - a.timestamp)
  return hits.slice(0, MAX_TOTAL_HITS)
}
