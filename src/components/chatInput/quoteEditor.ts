import type { DraftSegment } from '../../services/chat/drafts.js'

export type QuoteTokenClasses = {
  token: string
  icon: string
  quoteGlyph: string
  removeGlyph: string
  label: string
}

/** 展示宽度上限（全角/中文按 1 计，半角/ASCII 按 0.5 计），超出后显示省略号 */ 
const MAX_QUOTE_DISPLAY_WIDTH = 20
const SVG_NS = 'http://www.w3.org/2000/svg'

/** 压缩换行与多余空白，超出长度上限时以省略号截断展示（完整原文保留在 data-quote-text 中） */
export const formatQuoteDisplay = (text: string): string => {
  const compact = text.replace(/\s+/g, ' ').trim()
  let width = 0
  let result = ''
  for (const char of compact) {
    const step = char.charCodeAt(0) > 0xff ? 1 : 0.5
    if (width + step > MAX_QUOTE_DISPLAY_WIDTH) {
      return `${result.trimEnd()}...`
    }
    width += step
    result += char
  }
  return result
}

const createQuoteIconSvg = (): SVGSVGElement => {
  const svg = document.createElementNS(SVG_NS, 'svg')
  svg.setAttribute('width', '14')
  svg.setAttribute('height', '14')
  svg.setAttribute('viewBox', '0 0 16 16')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('aria-hidden', 'true')

  const path = document.createElementNS(SVG_NS, 'path')
  path.setAttribute(
    'd',
    'M6.8 3.2C4.15 4.42 2.5 6.72 2.5 9.55C2.5 11.42 3.74 12.7 5.38 12.7C6.82 12.7 7.9 11.63 7.9 10.22C7.9 8.88 6.92 7.88 5.66 7.82C5.45 7.81 5.24 7.83 5.04 7.89C5.29 6.38 6.24 5.17 7.58 4.44L6.8 3.2ZM13.5 3.2C10.85 4.42 9.2 6.72 9.2 9.55C9.2 11.42 10.44 12.7 12.08 12.7C13.52 12.7 14.6 11.63 14.6 10.22C14.6 8.88 13.62 7.88 12.36 7.82C12.15 7.81 11.94 7.83 11.74 7.89C11.99 6.38 12.94 5.17 14.28 4.44L13.5 3.2Z'
  )
  path.setAttribute('fill', 'currentColor')
  svg.appendChild(path)
  return svg
}

const createRemoveIconSvg = (): SVGSVGElement => {
  const svg = document.createElementNS(SVG_NS, 'svg')
  svg.setAttribute('width', '13')
  svg.setAttribute('height', '13')
  svg.setAttribute('viewBox', '0 0 16 16')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('aria-hidden', 'true')

  const path = document.createElementNS(SVG_NS, 'path')
  path.setAttribute('d', 'M3.75 3.75L12.25 12.25M12.25 3.75L3.75 12.25')
  path.setAttribute('stroke', 'currentColor')
  path.setAttribute('stroke-width', '1.5')
  path.setAttribute('stroke-linecap', 'round')
  svg.appendChild(path)
  return svg
}

export const createQuoteTokenElement = (
  id: string,
  text: string,
  classes: QuoteTokenClasses
): HTMLSpanElement => {
  const token = document.createElement('span')
  token.className = classes.token
  token.contentEditable = 'false'
  token.dataset.quoteId = id
  token.dataset.quoteText = text
  token.dataset.quotePreview = 'true'

  const icon = document.createElement('span')
  icon.className = classes.icon
  icon.dataset.quoteRemove = 'true'
  icon.setAttribute('role', 'button')
  icon.setAttribute('aria-label', 'Remove quote')

  const quoteGlyph = document.createElement('span')
  quoteGlyph.className = classes.quoteGlyph
  quoteGlyph.setAttribute('aria-hidden', 'true')
  quoteGlyph.appendChild(createQuoteIconSvg())

  const removeGlyph = document.createElement('span')
  removeGlyph.className = classes.removeGlyph
  removeGlyph.setAttribute('aria-hidden', 'true')
  removeGlyph.appendChild(createRemoveIconSvg())

  icon.appendChild(quoteGlyph)
  icon.appendChild(removeGlyph)

  const label = document.createElement('span')
  label.className = classes.label
  label.textContent = formatQuoteDisplay(text)

  token.appendChild(icon)
  token.appendChild(label)
  return token
}

/** 点击引用上的叉叉删除该引用节点，同时保留其他已输入文字 */
export const removeQuoteToken = (root: HTMLElement, token: HTMLElement): Range | null => {
  if (!root.contains(token)) {
    return null
  }
  const prev = token.previousSibling
  const next = token.nextSibling

  const range = document.createRange()
  range.setStartBefore(token)
  range.collapse(true)

  token.remove()

  // 仅清理紧跟在被删引用后的单个自动空格（若其处于开头或前方已有空白），其余文本原样保留
  if (next?.nodeType === Node.TEXT_NODE) {
    const nextVal = next.nodeValue ?? ''
    const prevEndsWithSpace =
      !prev || (prev.nodeType === Node.TEXT_NODE && (prev.nodeValue === '' || /\s$/.test(prev.nodeValue ?? '')))
    if (prevEndsWithSpace && nextVal.startsWith(' ')) {
      next.nodeValue = nextVal.slice(1)
      if (next.nodeValue === '') {
        next.parentNode?.removeChild(next)
      }
    }
  }

  root.focus({ preventScroll: true })
  const selection = window.getSelection()
  if (selection) {
    selection.removeAllRanges()
    selection.addRange(range)
  }
  return range.cloneRange()
}

const isQuoteElement = (node: Node | null): boolean =>
  node instanceof HTMLElement && typeof node.dataset.quoteId === 'string'

const BLOCK_TAGS = new Set(['DIV', 'P', 'LI'])

const pushTextSegment = (segments: DraftSegment[], value: string) => {
  if (!value) {
    return
  }
  const prev = segments.at(-1)
  if (prev?.type === 'text') {
    prev.text += value
  } else {
    segments.push({ type: 'text', text: value })
  }
}

/** 将 contentEditable DOM 解析为结构化 DraftSegment 列表 */
export const parseEditorSegments = (root: HTMLElement): DraftSegment[] => {
  const segments: DraftSegment[] = []

  const walk = (node: Node, isRootChild: boolean, index: number) => {
    if (node.nodeType === Node.TEXT_NODE) {
      pushTextSegment(segments, node.nodeValue ?? '')
      return
    }
    if (!(node instanceof HTMLElement)) {
      return
    }
    if (isQuoteElement(node)) {
      const id = node.dataset.quoteId ?? ''
      const text = node.dataset.quoteText ?? ''
      if (text) {
        segments.push({ type: 'quote', id, text })
      }
      return
    }
    if (node.tagName === 'BR') {
      pushTextSegment(segments, '\n')
      return
    }
    if (isRootChild && index > 0 && BLOCK_TAGS.has(node.tagName)) {
      pushTextSegment(segments, '\n')
    }
    const children = Array.from(node.childNodes)
    for (let i = 0; i < children.length; i++) {
      walk(children[i], false, i)
    }
  }

  const topChildren = Array.from(root.childNodes)
  for (let i = 0; i < topChildren.length; i++) {
    walk(topChildren[i], true, i)
  }

  // WebKit/Chrome 在清空 contentEditable 后常残留一个孤立 <br>，等价于空内容
  if (segments.length === 1 && segments[0].type === 'text' && segments[0].text === '\n') {
    return []
  }
  return segments
}

/** 根据 DraftSegment 列表重建 contentEditable DOM */
export const renderEditorSegments = (
  root: HTMLElement,
  segments: DraftSegment[],
  classes: QuoteTokenClasses
) => {
  root.replaceChildren()
  for (const segment of segments) {
    if (segment.type === 'quote') {
      root.appendChild(createQuoteTokenElement(segment.id, segment.text, classes))
      continue
    }
    const lines = segment.text.split('\n')
    for (let i = 0; i < lines.length; i++) {
      if (i > 0) {
        root.appendChild(document.createElement('br'))
      }
      if (lines[i]) {
        root.appendChild(document.createTextNode(lines[i]))
      }
    }
  }
}

export const moveCaretToEnd = (root: HTMLElement) => {
  const selection = window.getSelection()
  if (!selection) {
    return
  }
  const range = document.createRange()
  range.selectNodeContents(root)
  range.collapse(false)
  selection.removeAllRanges()
  selection.addRange(range)
}

const isRangeInsideRoot = (range: Range | null, root: HTMLElement): range is Range =>
  Boolean(
    range &&
      root.contains(range.startContainer) &&
      root.contains(range.endContainer)
  )

const needsLeadingSpace = (root: HTMLElement, range: Range): boolean => {
  const { startContainer, startOffset } = range
  if (startContainer.nodeType === Node.TEXT_NODE) {
    const before = (startContainer.nodeValue ?? '').slice(0, startOffset)
    if (before.length > 0) {
      return !/\s$/.test(before)
    }
    const prev = startContainer.previousSibling
    if (isQuoteElement(prev)) {
      return true
    }
    if (prev?.nodeType === Node.TEXT_NODE) {
      const value = prev.nodeValue ?? ''
      return value.length > 0 && !/\s$/.test(value)
    }
    return false
  }
  if (startContainer === root && startOffset > 0) {
    const prev = root.childNodes[startOffset - 1]
    if (isQuoteElement(prev)) {
      return true
    }
    if (prev?.nodeType === Node.TEXT_NODE) {
      const value = prev.nodeValue ?? ''
      return value.length > 0 && !/\s$/.test(value)
    }
  }
  return false
}

/**
 * 在上次记录的光标位置（或末尾）插入一个内联引用节点，
 * 并在其后放置一个空格文本节点与光标，保证连续输入中文 IME 和多次引用顺滑自然。
 */
export const insertQuoteAtRange = (
  root: HTMLElement,
  savedRange: Range | null,
  id: string,
  text: string,
  classes: QuoteTokenClasses
): Range | null => {
  let range: Range
  if (isRangeInsideRoot(savedRange, root)) {
    range = savedRange.cloneRange()
  } else {
    range = document.createRange()
    range.selectNodeContents(root)
    range.collapse(false)
  }

  // 清除孤立的占位 <br>
  if (root.childNodes.length === 1 && root.firstChild?.nodeName === 'BR') {
    root.replaceChildren()
    range.selectNodeContents(root)
    range.collapse(false)
  }

  range.deleteContents()

  const addSpaceBefore = needsLeadingSpace(root, range)
  const token = createQuoteTokenElement(id, text, classes)
  const trailingSpace = document.createTextNode(' ')

  const fragment = document.createDocumentFragment()
  if (addSpaceBefore) {
    fragment.appendChild(document.createTextNode(' '))
  }
  fragment.appendChild(token)
  fragment.appendChild(trailingSpace)

  range.insertNode(fragment)

  const nextRange = document.createRange()
  nextRange.setStart(trailingSpace, trailingSpace.length)
  nextRange.collapse(true)

  root.focus({ preventScroll: true })
  const selection = window.getSelection()
  if (selection) {
    selection.removeAllRanges()
    selection.addRange(nextRange)
  }
  return nextRange.cloneRange()
}

/** 在当前光标（或上次记录的位置）插入纯文本，用于粘贴与语音转写 */
export const insertPlainTextAtRange = (
  root: HTMLElement,
  savedRange: Range | null,
  text: string
): Range | null => {
  const selection = window.getSelection()
  let range: Range | null = null
  if (selection && selection.rangeCount > 0) {
    const live = selection.getRangeAt(0)
    if (isRangeInsideRoot(live, root)) {
      range = live.cloneRange()
    }
  }
  if (!range) {
    if (isRangeInsideRoot(savedRange, root)) {
      range = savedRange.cloneRange()
    } else {
      range = document.createRange()
      range.selectNodeContents(root)
      range.collapse(false)
    }
  }

  range.deleteContents()

  const lines = text.split(/\r?\n/)
  const fragment = document.createDocumentFragment()
  let lastNode: Node | null = null
  for (let i = 0; i < lines.length; i++) {
    if (i > 0) {
      const br = document.createElement('br')
      fragment.appendChild(br)
      lastNode = br
    }
    if (lines[i]) {
      const textNode = document.createTextNode(lines[i])
      fragment.appendChild(textNode)
      lastNode = textNode
    }
  }
  if (!lastNode) {
    return range
  }

  range.insertNode(fragment)

  const nextRange = document.createRange()
  if (lastNode.nodeType === Node.TEXT_NODE) {
    nextRange.setStart(lastNode, (lastNode.nodeValue ?? '').length)
  } else {
    nextRange.setStartAfter(lastNode)
  }
  nextRange.collapse(true)

  root.focus({ preventScroll: true })
  if (selection) {
    selection.removeAllRanges()
    selection.addRange(nextRange)
  }
  return nextRange.cloneRange()
}

/**
 * 在引用节点紧邻处按 Backspace / Delete 时，整体删除该引用节点，
 * 避免不同浏览器对 contenteditable="false" 的删除行为不一致。
 */
export const handleAtomicQuoteDeletion = (
  root: HTMLElement,
  direction: 'backward' | 'forward'
): boolean => {
  const selection = window.getSelection()
  if (!selection || !selection.isCollapsed || selection.rangeCount === 0) {
    return false
  }
  const range = selection.getRangeAt(0)
  if (!isRangeInsideRoot(range, root)) {
    return false
  }
  const { startContainer, startOffset } = range
  let target: HTMLElement | null = null

  if (direction === 'backward') {
    if (startContainer === root && startOffset > 0) {
      const prev = root.childNodes[startOffset - 1]
      if (prev instanceof HTMLElement && isQuoteElement(prev)) {
        target = prev
      }
    } else if (startContainer.nodeType === Node.TEXT_NODE && startOffset === 0) {
      const prev = startContainer.previousSibling
      if (prev instanceof HTMLElement && isQuoteElement(prev)) {
        target = prev
      }
    }
  } else {
    if (startContainer === root && startOffset < root.childNodes.length) {
      const next = root.childNodes[startOffset]
      if (next instanceof HTMLElement && isQuoteElement(next)) {
        target = next
      }
    } else if (
      startContainer.nodeType === Node.TEXT_NODE &&
      startOffset === (startContainer.nodeValue ?? '').length
    ) {
      const next = startContainer.nextSibling
      if (next instanceof HTMLElement && isQuoteElement(next)) {
        target = next
      }
    }
  }

  if (!target) {
    return false
  }

  const nextRange = document.createRange()
  nextRange.setStartBefore(target)
  nextRange.collapse(true)
  target.remove()
  selection.removeAllRanges()
  selection.addRange(nextRange)
  return true
}

export const isCaretAtStart = (root: HTMLElement): boolean => {
  const selection = window.getSelection()
  if (!selection || !selection.isCollapsed || selection.rangeCount === 0) {
    return false
  }
  const range = selection.getRangeAt(0)
  if (!isRangeInsideRoot(range, root)) {
    return false
  }
  const probe = document.createRange()
  probe.setStart(root, 0)
  probe.setEnd(range.startContainer, range.startOffset)
  if (probe.toString().length > 0) {
    return false
  }
  const fragment = probe.cloneContents()
  return fragment.querySelector('[data-quote-id], br') === null
}

export const isCaretAtEnd = (root: HTMLElement): boolean => {
  const selection = window.getSelection()
  if (!selection || !selection.isCollapsed || selection.rangeCount === 0) {
    return false
  }
  const range = selection.getRangeAt(0)
  if (!isRangeInsideRoot(range, root)) {
    return false
  }
  const probe = document.createRange()
  probe.setStart(range.endContainer, range.endOffset)
  probe.setEnd(root, root.childNodes.length)
  if (probe.toString().length > 0) {
    return false
  }
  const fragment = probe.cloneContents()
  return fragment.querySelector('[data-quote-id], br') === null
}
