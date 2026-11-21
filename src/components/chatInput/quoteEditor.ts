import type { DraftSegment } from '../../services/chat/drafts.js'

export type QuoteTokenClasses = {
  token: string
  icon: string
  quoteGlyph: string
  removeGlyph: string
  label: string
  skillToken: string
  skillIcon: string
  skillGlyph: string
  skillLabel: string
}

export type SlashCaretMatch = {
  query: string
  textNode: Text
  slashOffset: number
  endOffset: number
}

/** 展示宽度上限（全角/中文按 1 计，半角/ASCII 按 0.5 计），超出后显示省略号 */
const MAX_QUOTE_DISPLAY_WIDTH = 20
const SVG_NS = 'http://www.w3.org/2000/svg'
/** WebKit 在 contenteditable="false" 节点前需要一个可编辑文本槽位才能落入光标 */
const CARET_ANCHOR = '\u200B'

const stripCaretAnchors = (value: string): string => value.replace(/\u200B/g, '')

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

const createSkillIconSvg = (): SVGSVGElement => {
  const svg = document.createElementNS(SVG_NS, 'svg')
  svg.setAttribute('width', '15')
  svg.setAttribute('height', '15')
  svg.setAttribute('viewBox', '0 0 16 16')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('aria-hidden', 'true')

  const mainStar = document.createElementNS(SVG_NS, 'path')
  mainStar.setAttribute(
    'd',
    'M6.8 1.8L8.05 5.45L11.7 6.7L8.05 7.95L6.8 11.6L5.55 7.95L1.9 6.7L5.55 5.45L6.8 1.8Z'
  )
  mainStar.setAttribute('stroke', 'currentColor')
  mainStar.setAttribute('stroke-width', '1.3')
  mainStar.setAttribute('stroke-linejoin', 'round')
  svg.appendChild(mainStar)

  const smallStar = document.createElementNS(SVG_NS, 'path')
  smallStar.setAttribute(
    'd',
    'M12.3 9.7L12.9 11.4L14.6 12L12.9 12.6L12.3 14.3L11.7 12.6L10 12L11.7 11.4L12.3 9.7Z'
  )
  smallStar.setAttribute('fill', 'currentColor')
  svg.appendChild(smallStar)
  return svg
}

const createCommandIconSvg = (): SVGSVGElement => {
  const svg = document.createElementNS(SVG_NS, 'svg')
  svg.setAttribute('width', '15')
  svg.setAttribute('height', '15')
  svg.setAttribute('viewBox', '0 0 16 16')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('aria-hidden', 'true')

  const box = document.createElementNS(SVG_NS, 'rect')
  box.setAttribute('x', '2')
  box.setAttribute('y', '2')
  box.setAttribute('width', '12')
  box.setAttribute('height', '12')
  box.setAttribute('rx', '3.5')
  box.setAttribute('stroke', 'currentColor')
  box.setAttribute('stroke-width', '1.3')
  svg.appendChild(box)

  const slash = document.createElementNS(SVG_NS, 'path')
  slash.setAttribute('d', 'M9.6 4.7L6.4 11.3')
  slash.setAttribute('stroke', 'currentColor')
  slash.setAttribute('stroke-width', '1.3')
  slash.setAttribute('stroke-linecap', 'round')
  svg.appendChild(slash)
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

export const createSkillTokenElement = (
  id: string,
  name: string,
  classes: QuoteTokenClasses,
  kind: 'skill' | 'command' = 'skill'
): HTMLSpanElement => {
  const token = document.createElement('span')
  token.className = classes.skillToken
  token.contentEditable = 'false'
  token.dataset.skillId = id
  token.dataset.skillName = name
  token.dataset.skillKind = kind

  const icon = document.createElement('span')
  icon.className = classes.skillIcon
  icon.dataset.skillRemove = 'true'
  icon.setAttribute('role', 'button')
  icon.setAttribute('aria-label', `Remove ${kind} ${name}`)

  const skillGlyph = document.createElement('span')
  skillGlyph.className = classes.skillGlyph
  skillGlyph.setAttribute('aria-hidden', 'true')
  skillGlyph.appendChild(kind === 'command' ? createCommandIconSvg() : createSkillIconSvg())

  const removeGlyph = document.createElement('span')
  removeGlyph.className = classes.removeGlyph
  removeGlyph.setAttribute('aria-hidden', 'true')
  removeGlyph.appendChild(createRemoveIconSvg())

  icon.appendChild(skillGlyph)
  icon.appendChild(removeGlyph)

  const label = document.createElement('span')
  label.className = classes.skillLabel
  label.textContent = name

  token.appendChild(icon)
  token.appendChild(label)
  return token
}

/** 点击引用或技能胶囊上的叉叉删除该节点，同时保留其他已输入文字 */
export const removeQuoteToken = (root: HTMLElement, token: HTMLElement): Range | null => {
  if (!root.contains(token)) {
    return null
  }
  const prev = token.previousSibling
  const next = token.nextSibling
  const prevAnchor =
    prev?.nodeType === Node.TEXT_NODE && prev.nodeValue === CARET_ANCHOR ? prev : null

  const range = document.createRange()
  range.setStartBefore(prevAnchor ?? token)
  range.collapse(true)

  token.remove()
  prevAnchor?.remove()

  // 仅清理紧跟在被删节点后的单个自动空格（若其处于开头或前方已有空白），其余文本原样保留
  if (next?.nodeType === Node.TEXT_NODE) {
    const nextVal = next.nodeValue ?? ''
    const beforeNode = prevAnchor ? prevAnchor.previousSibling : prev
    const prevEndsWithSpace =
      !beforeNode ||
      (beforeNode.nodeType === Node.TEXT_NODE &&
        (stripCaretAnchors(beforeNode.nodeValue ?? '') === '' ||
          /\s$/.test(stripCaretAnchors(beforeNode.nodeValue ?? ''))))
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

const isSkillElement = (node: Node | null): boolean =>
  node instanceof HTMLElement && typeof node.dataset.skillId === 'string'

const isInlineTokenElement = (node: Node | null): node is HTMLElement =>
  isQuoteElement(node) || isSkillElement(node)

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
      pushTextSegment(segments, stripCaretAnchors(node.nodeValue ?? ''))
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
    if (isSkillElement(node)) {
      const id = node.dataset.skillId ?? ''
      const name = node.dataset.skillName ?? ''
      const kind = node.dataset.skillKind === 'command' ? 'command' : 'skill'
      if (name) {
        segments.push({ type: 'skill', id, name, kind })
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
    if (segment.type === 'quote' || segment.type === 'skill') {
      const prev = root.lastChild
      if (!prev || prev.nodeType !== Node.TEXT_NODE) {
        root.appendChild(document.createTextNode(CARET_ANCHOR))
      }
      root.appendChild(
        segment.type === 'quote'
          ? createQuoteTokenElement(segment.id, segment.text, classes)
          : createSkillTokenElement(segment.id, segment.name, classes, segment.kind ?? 'skill')
      )
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
  Boolean(range && root.contains(range.startContainer) && root.contains(range.endContainer))

/** 检测当前光标前是否正在输入 `/skill-prefix`（支持句首与句中任意位置） */
export const getSlashMatchAtCaret = (root: HTMLElement): SlashCaretMatch | null => {
  const selection = window.getSelection()
  if (!selection || !selection.isCollapsed || selection.rangeCount === 0) {
    return null
  }
  const range = selection.getRangeAt(0)
  if (!isRangeInsideRoot(range, root)) {
    return null
  }
  let textNode: Text | null = null
  let endOffset = 0
  if (range.startContainer.nodeType === Node.TEXT_NODE) {
    textNode = range.startContainer as Text
    endOffset = range.startOffset
  } else if (range.startContainer === root && range.startOffset > 0) {
    const prev = root.childNodes[range.startOffset - 1]
    if (prev?.nodeType === Node.TEXT_NODE) {
      textNode = prev as Text
      endOffset = (textNode.nodeValue ?? '').length
    }
  }
  if (!textNode) {
    return null
  }
  const before = (textNode.nodeValue ?? '').slice(0, endOffset)
  const match = before.match(/(?:^|[\s\u200B])\/([A-Za-z0-9._-]*)$/)
  if (!match) {
    return null
  }
  const query = match[1]
  const slashOffset = endOffset - query.length - 1
  return {
    query: query.toLowerCase(),
    textNode,
    slashOffset,
    endOffset,
  }
}

/** 将当前光标处的 `/query` 替换为内联技能胶囊，并在其后插入空格方便继续输入 */
export const replaceSlashWithSkillToken = (
  root: HTMLElement,
  match: SlashCaretMatch,
  id: string,
  name: string,
  classes: QuoteTokenClasses,
  kind: 'skill' | 'command' = 'skill'
): Range | null => {
  if (!root.contains(match.textNode)) {
    return null
  }
  const range = document.createRange()
  range.setStart(match.textNode, match.slashOffset)
  range.setEnd(match.textNode, match.endOffset)
  range.deleteContents()

  const token = createSkillTokenElement(id, name, classes, kind)
  const trailingSpace = document.createTextNode(' ')
  const fragment = document.createDocumentFragment()
  // 句首或紧邻其他内联节点时，在胶囊前放置零宽锚点，确保 WebKit 光标可移至胶囊最左侧。
  if (
    match.slashOffset === 0 &&
    (!match.textNode.previousSibling || match.textNode.previousSibling.nodeType !== Node.TEXT_NODE)
  ) {
    fragment.appendChild(document.createTextNode(CARET_ANCHOR))
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

const needsLeadingSpace = (root: HTMLElement, range: Range): boolean => {
  const { startContainer, startOffset } = range
  if (startContainer.nodeType === Node.TEXT_NODE) {
    const before = stripCaretAnchors((startContainer.nodeValue ?? '').slice(0, startOffset))
    if (before.length > 0) {
      return !/\s$/.test(before)
    }
    const prev = startContainer.previousSibling
    if (isInlineTokenElement(prev)) {
      return true
    }
    if (prev?.nodeType === Node.TEXT_NODE) {
      const value = stripCaretAnchors(prev.nodeValue ?? '')
      return value.length > 0 && !/\s$/.test(value)
    }
    return false
  }
  if (startContainer === root && startOffset > 0) {
    const prev = root.childNodes[startOffset - 1]
    if (isInlineTokenElement(prev)) {
      return true
    }
    if (prev?.nodeType === Node.TEXT_NODE) {
      const value = stripCaretAnchors(prev.nodeValue ?? '')
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
  } else if (range.startOffset === 0) {
    fragment.appendChild(document.createTextNode(CARET_ANCHOR))
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

const ensureTextSlotBefore = (token: HTMLElement): Text => {
  const prev = token.previousSibling
  if (prev?.nodeType === Node.TEXT_NODE) {
    return prev as Text
  }
  const anchor = document.createTextNode(CARET_ANCHOR)
  token.parentNode?.insertBefore(anchor, token)
  return anchor
}

const ensureTextSlotAfter = (token: HTMLElement): Text => {
  const next = token.nextSibling
  if (next?.nodeType === Node.TEXT_NODE) {
    return next as Text
  }
  const anchor = document.createTextNode(CARET_ANCHOR)
  token.parentNode?.insertBefore(anchor, token.nextSibling)
  return anchor
}

const findAdjacentInlineToken = (
  root: HTMLElement,
  range: Range,
  direction: 'backward' | 'forward'
): HTMLElement | null => {
  const { startContainer, startOffset } = range
  if (direction === 'backward') {
    if (startContainer === root && startOffset > 0) {
      const prev = root.childNodes[startOffset - 1]
      return isInlineTokenElement(prev) ? prev : null
    }
    if (startContainer.nodeType === Node.TEXT_NODE) {
      const before = stripCaretAnchors((startContainer.nodeValue ?? '').slice(0, startOffset))
      if (before.length === 0) {
        const prev = startContainer.previousSibling
        return isInlineTokenElement(prev) ? prev : null
      }
    }
    return null
  }
  if (startContainer === root && startOffset < root.childNodes.length) {
    const next = root.childNodes[startOffset]
    return isInlineTokenElement(next) ? next : null
  }
  if (startContainer.nodeType === Node.TEXT_NODE) {
    const after = stripCaretAnchors((startContainer.nodeValue ?? '').slice(startOffset))
    if (after.length === 0) {
      const next = startContainer.nextSibling
      return isInlineTokenElement(next) ? next : null
    }
  }
  return null
}

/** 使用左右方向键跨越内联胶囊，确保光标能顺滑移到句首胶囊最左侧 */
export const handleHorizontalTokenNavigation = (
  root: HTMLElement,
  direction: 'left' | 'right'
): boolean => {
  const selection = window.getSelection()
  if (!selection || !selection.isCollapsed || selection.rangeCount === 0) {
    return false
  }
  const range = selection.getRangeAt(0)
  if (!isRangeInsideRoot(range, root)) {
    return false
  }
  const token = findAdjacentInlineToken(root, range, direction === 'left' ? 'backward' : 'forward')
  if (!token) {
    return false
  }
  const nextRange = document.createRange()
  if (direction === 'left') {
    const slot = ensureTextSlotBefore(token)
    const len = (slot.nodeValue ?? '').length
    nextRange.setStart(slot, slot.nodeValue === CARET_ANCHOR ? 0 : len)
  } else {
    const slot = ensureTextSlotAfter(token)
    nextRange.setStart(slot, slot.nodeValue === CARET_ANCHOR ? 1 : 0)
  }
  nextRange.collapse(true)
  selection.removeAllRanges()
  selection.addRange(nextRange)
  return true
}

/** 点击首行最左侧空白（首个胶囊之前）时将光标放到句首 */
export const placeCaretBeforeLeadingTokenIfClicked = (
  root: HTMLElement,
  clientX: number,
  clientY: number
): boolean => {
  const firstToken = Array.from(root.childNodes).find((node) => isInlineTokenElement(node))
  if (!firstToken || !isInlineTokenElement(firstToken)) {
    return false
  }
  // 仅当首个胶囊之前没有可见文字时生效
  for (let n = root.firstChild; n && n !== firstToken; n = n.nextSibling) {
    if (n.nodeType === Node.TEXT_NODE && stripCaretAnchors(n.nodeValue ?? '').length > 0) {
      return false
    }
    if (n.nodeType !== Node.TEXT_NODE) {
      return false
    }
  }
  const rect = firstToken.getBoundingClientRect()
  if (clientX <= rect.left + 4 && clientY >= rect.top - 8 && clientY <= rect.bottom + 8) {
    const slot = ensureTextSlotBefore(firstToken)
    const range = document.createRange()
    range.setStart(slot, 0)
    range.collapse(true)
    root.focus({ preventScroll: true })
    const selection = window.getSelection()
    if (selection) {
      selection.removeAllRanges()
      selection.addRange(range)
    }
    return true
  }
  return false
}

/**
 * 在引用或技能胶囊紧邻处按 Backspace / Delete 时，整体删除该节点，
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
  const target = findAdjacentInlineToken(root, range, direction)
  if (!target) {
    return false
  }

  const prevAnchor =
    target.previousSibling?.nodeType === Node.TEXT_NODE &&
    target.previousSibling.nodeValue === CARET_ANCHOR
      ? target.previousSibling
      : null

  const nextRange = document.createRange()
  nextRange.setStartBefore(prevAnchor ?? target)
  nextRange.collapse(true)
  target.remove()
  prevAnchor?.remove()
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
  if (stripCaretAnchors(probe.toString()).length > 0) {
    return false
  }
  const fragment = probe.cloneContents()
  return fragment.querySelector('[data-quote-id], [data-skill-id], br') === null
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
  if (stripCaretAnchors(probe.toString()).length > 0) {
    return false
  }
  const fragment = probe.cloneContents()
  return fragment.querySelector('[data-quote-id], [data-skill-id], br') === null
}
