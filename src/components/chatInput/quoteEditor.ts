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
  fileToken: string
  fileIcon: string
  fileGlyph: string
  fileLabel: string
}

export type SlashCaretMatch = {
  query: string
  textNode: Text
  slashOffset: number
  endOffset: number
}

export type AtCaretMatch = {
  query: string
  textNode: Text
  atOffset: number
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

const createFileIconSvg = (): SVGSVGElement => {
  const svg = document.createElementNS(SVG_NS, 'svg')
  svg.setAttribute('width', '15')
  svg.setAttribute('height', '15')
  svg.setAttribute('viewBox', '0 0 16 16')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('aria-hidden', 'true')

  const rect = document.createElementNS(SVG_NS, 'rect')
  rect.setAttribute('x', '2.4')
  rect.setAttribute('y', '2.4')
  rect.setAttribute('width', '11.2')
  rect.setAttribute('height', '11.2')
  rect.setAttribute('rx', '2.2')
  rect.setAttribute('stroke', 'currentColor')
  rect.setAttribute('stroke-width', '1.3')
  svg.appendChild(rect)

  const lines = document.createElementNS(SVG_NS, 'path')
  lines.setAttribute('d', 'M5.2 6.2H10.8M5.2 9.4H8.6')
  lines.setAttribute('stroke', 'currentColor')
  lines.setAttribute('stroke-width', '1.3')
  lines.setAttribute('stroke-linecap', 'round')
  svg.appendChild(lines)
  return svg
}

const createFolderIconSvg = (): SVGSVGElement => {
  const svg = document.createElementNS(SVG_NS, 'svg')
  svg.setAttribute('width', '15')
  svg.setAttribute('height', '15')
  svg.setAttribute('viewBox', '0 0 16 16')
  svg.setAttribute('fill', 'none')
  svg.setAttribute('aria-hidden', 'true')

  const path = document.createElementNS(SVG_NS, 'path')
  path.setAttribute(
    'd',
    'M2.2 4.3C2.2 3.47 2.87 2.8 3.7 2.8H6.15C6.57 2.8 6.97 2.98 7.25 3.3L8.05 4.2H12.3C13.13 4.2 13.8 4.87 13.8 5.7V11.7C13.8 12.53 13.13 13.2 12.3 13.2H3.7C2.87 13.2 2.2 12.53 2.2 11.7V4.3Z'
  )
  path.setAttribute('stroke', 'currentColor')
  path.setAttribute('stroke-width', '1.3')
  path.setAttribute('stroke-linejoin', 'round')
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

export const createFileTokenElement = (
  id: string,
  path: string,
  name: string,
  kind: 'file' | 'directory',
  classes: QuoteTokenClasses
): HTMLSpanElement => {
  const token = document.createElement('span')
  token.className = classes.fileToken
  token.contentEditable = 'false'
  token.dataset.fileId = id
  token.dataset.filePath = path
  token.dataset.fileName = name
  token.dataset.fileKind = kind
  token.title = path

  const icon = document.createElement('span')
  icon.className = classes.fileIcon
  icon.dataset.fileRemove = 'true'
  icon.setAttribute('role', 'button')
  icon.setAttribute('aria-label', `Remove ${kind} ${path}`)

  const fileGlyph = document.createElement('span')
  fileGlyph.className = classes.fileGlyph
  fileGlyph.setAttribute('aria-hidden', 'true')
  fileGlyph.appendChild(kind === 'directory' ? createFolderIconSvg() : createFileIconSvg())

  const removeGlyph = document.createElement('span')
  removeGlyph.className = classes.removeGlyph
  removeGlyph.setAttribute('aria-hidden', 'true')
  removeGlyph.appendChild(createRemoveIconSvg())

  icon.appendChild(fileGlyph)
  icon.appendChild(removeGlyph)

  const label = document.createElement('span')
  label.className = classes.fileLabel
  label.textContent = kind === 'directory' ? `${name}/` : name

  token.appendChild(icon)
  token.appendChild(label)
  return token
}

/** 点击引用、技能或文件胶囊上的叉叉删除该节点，同时保留其他已输入文字 */
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

const isFileElement = (node: Node | null): boolean =>
  node instanceof HTMLElement && typeof node.dataset.fileId === 'string'

const isInlineTokenElement = (node: Node | null): node is HTMLElement =>
  isQuoteElement(node) || isSkillElement(node) || isFileElement(node)

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
    if (isFileElement(node)) {
      const id = node.dataset.fileId ?? ''
      const path = node.dataset.filePath ?? ''
      const name = node.dataset.fileName ?? ''
      const kind = node.dataset.fileKind === 'directory' ? 'directory' : 'file'
      if (path && name) {
        segments.push({ type: 'file', id, path, name, kind })
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
    if (segment.type === 'quote' || segment.type === 'skill' || segment.type === 'file') {
      const prev = root.lastChild
      if (!prev || prev.nodeType !== Node.TEXT_NODE) {
        root.appendChild(document.createTextNode(CARET_ANCHOR))
      }
      root.appendChild(
        segment.type === 'quote'
          ? createQuoteTokenElement(segment.id, segment.text, classes)
          : segment.type === 'skill'
            ? createSkillTokenElement(segment.id, segment.name, classes, segment.kind ?? 'skill')
            : createFileTokenElement(segment.id, segment.path, segment.name, segment.kind, classes)
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
  if (match.textNode.parentNode && match.textNode.nodeValue === '') {
    match.textNode.parentNode.removeChild(match.textNode)
  }

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

/** 检测当前光标前是否正在输入 `@path-query`（支持句首与句中任意位置） */
export const getAtMatchAtCaret = (root: HTMLElement): AtCaretMatch | null => {
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
  const match = before.match(/(?:^|[\s\u200B])@([^\s\u200B]*)$/)
  if (!match) {
    return null
  }
  const query = match[1]
  const atOffset = endOffset - query.length - 1
  return {
    query,
    textNode,
    atOffset,
    endOffset,
  }
}

/** 在目录 Tab 钻取或点击面包屑时，将 `@oldQuery` 原地替换为 `@nextQuery` 并保持光标在末尾 */
export const replaceAtQueryText = (
  root: HTMLElement,
  match: AtCaretMatch,
  nextQuery: string
): Range | null => {
  if (!root.contains(match.textNode)) {
    return null
  }
  const value = match.textNode.nodeValue ?? ''
  const replacement = `@${nextQuery}`
  match.textNode.nodeValue =
    value.slice(0, match.atOffset) + replacement + value.slice(match.endOffset)

  const nextOffset = match.atOffset + replacement.length
  const nextRange = document.createRange()
  nextRange.setStart(match.textNode, nextOffset)
  nextRange.collapse(true)

  root.focus({ preventScroll: true })
  const selection = window.getSelection()
  if (selection) {
    selection.removeAllRanges()
    selection.addRange(nextRange)
  }
  return nextRange.cloneRange()
}

/** 将当前光标处的 `@query` 替换为内联文件/目录引用胶囊，并在其后插入空格 */
export const replaceAtWithFileToken = (
  root: HTMLElement,
  match: AtCaretMatch,
  id: string,
  path: string,
  name: string,
  kind: 'file' | 'directory',
  classes: QuoteTokenClasses
): Range | null => {
  if (!root.contains(match.textNode)) {
    return null
  }
  const range = document.createRange()
  range.setStart(match.textNode, match.atOffset)
  range.setEnd(match.textNode, match.endOffset)
  range.deleteContents()

  const token = createFileTokenElement(id, path, name, kind, classes)
  const trailingSpace = document.createTextNode(' ')
  const fragment = document.createDocumentFragment()
  if (
    match.atOffset === 0 &&
    (!match.textNode.previousSibling || match.textNode.previousSibling.nodeType !== Node.TEXT_NODE)
  ) {
    fragment.appendChild(document.createTextNode(CARET_ANCHOR))
  }
  fragment.appendChild(token)
  fragment.appendChild(trailingSpace)

  range.insertNode(fragment)
  if (match.textNode.parentNode && match.textNode.nodeValue === '') {
    match.textNode.parentNode.removeChild(match.textNode)
  }

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

const isEmptyOrAnchorText = (node: Node | null): boolean =>
  node?.nodeType === Node.TEXT_NODE && stripCaretAnchors(node.nodeValue ?? '').length === 0

const findSiblingInlineToken = (
  start: Node | null,
  direction: 'backward' | 'forward'
): HTMLElement | null => {
  let cursor = start
  while (cursor && isEmptyOrAnchorText(cursor)) {
    cursor = direction === 'backward' ? cursor.previousSibling : cursor.nextSibling
  }
  return isInlineTokenElement(cursor) ? cursor : null
}

const findAdjacentInlineToken = (
  root: HTMLElement,
  range: Range,
  direction: 'backward' | 'forward'
): HTMLElement | null => {
  const { startContainer, startOffset } = range
  if (direction === 'backward') {
    if (startContainer === root && startOffset > 0) {
      return findSiblingInlineToken(root.childNodes[startOffset - 1] ?? null, 'backward')
    }
    if (startContainer.nodeType === Node.TEXT_NODE) {
      const before = stripCaretAnchors((startContainer.nodeValue ?? '').slice(0, startOffset))
      if (before.length === 0) {
        return findSiblingInlineToken(startContainer.previousSibling, 'backward')
      }
    }
    return null
  }
  if (startContainer === root && startOffset < root.childNodes.length) {
    return findSiblingInlineToken(root.childNodes[startOffset] ?? null, 'forward')
  }
  if (startContainer.nodeType === Node.TEXT_NODE) {
    const after = stripCaretAnchors((startContainer.nodeValue ?? '').slice(startOffset))
    if (after.length === 0) {
      return findSiblingInlineToken(startContainer.nextSibling, 'forward')
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
 * 在引用、技能或文件胶囊紧邻处按 Backspace / Delete 时，整体删除该节点，
 * 同时拦截 WebKit 删除胶囊后最后一个字符时把光标跳回句首的异常行为。
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

  // WebKit 在删掉紧跟 contenteditable="false" 胶囊后的最后一个字符（例如自动空格）时，
  // 会错误地把光标跳到行首。手动删除该字符并保留零宽槽位可让光标稳稳停在胶囊右侧。
  if (direction === 'backward' && range.startContainer.nodeType === Node.TEXT_NODE) {
    const textNode = range.startContainer as Text
    const value = textNode.nodeValue ?? ''
    const beforeRaw = value.slice(0, range.startOffset)
    const afterRaw = value.slice(range.startOffset)
    if (
      stripCaretAnchors(beforeRaw).length === 1 &&
      findSiblingInlineToken(textNode.previousSibling, 'backward') !== null
    ) {
      textNode.nodeValue = `${CARET_ANCHOR}${afterRaw}`
      const nextRange = document.createRange()
      nextRange.setStart(textNode, CARET_ANCHOR.length)
      nextRange.collapse(true)
      selection.removeAllRanges()
      selection.addRange(nextRange)
      return true
    }
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

  const prevSibling = (prevAnchor ?? target).previousSibling
  target.remove()
  prevAnchor?.remove()

  const nextRange = document.createRange()
  if (prevSibling && isInlineTokenElement(prevSibling)) {
    const slot = ensureTextSlotAfter(prevSibling)
    nextRange.setStart(slot, slot.nodeValue === CARET_ANCHOR ? CARET_ANCHOR.length : 0)
  } else if (prevSibling?.nodeType === Node.TEXT_NODE) {
    nextRange.setStart(prevSibling, (prevSibling.nodeValue ?? '').length)
  } else {
    nextRange.setStart(root, 0)
  }
  nextRange.collapse(true)
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
  return fragment.querySelector('[data-quote-id], [data-skill-id], [data-file-id], br') === null
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
  return fragment.querySelector('[data-quote-id], [data-skill-id], [data-file-id], br') === null
}
