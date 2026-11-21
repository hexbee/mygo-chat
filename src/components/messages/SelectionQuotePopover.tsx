import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { useTranslate } from '../../i18n/index.js'
import { requestInsertQuote } from '../../services/chat/drafts.js'
import styles from './SelectionQuotePopover.module.css'

type PopoverState = {
  text: string
  top: number
  left: number
}

const POPOVER_HEIGHT = 36
const POPOVER_ESTIMATED_HALF_WIDTH = 52
const VERTICAL_GAP = 8
const VIEWPORT_MARGIN = 12
const TOP_SAFE_BOUNDARY = 56

const isExcludedNode = (node: Node | null): boolean => {
  const el = node instanceof Element ? node : node?.parentElement
  if (!el) {
    return true
  }
  return Boolean(
    el.closest('textarea, input, button, [contenteditable="true"], [data-no-quote="true"]')
  )
}

/** 计算消息列表实际可见区域（避开顶栏与底部吸底输入框） */
const getVisibleBounds = (scrollEl: HTMLElement | null) => {
  const scrollRect = scrollEl?.getBoundingClientRect()
  const top = scrollRect ? Math.max(scrollRect.top, TOP_SAFE_BOUNDARY) : TOP_SAFE_BOUNDARY
  let bottom = scrollRect ? scrollRect.bottom : window.innerHeight

  if (scrollEl) {
    const lastChild = scrollEl.lastElementChild
    if (lastChild instanceof HTMLElement && lastChild !== scrollEl.firstElementChild) {
      const footerTop = lastChild.getBoundingClientRect().top
      if (footerTop > top && footerTop < bottom) {
        bottom = footerTop
      }
    }
  }

  return {
    top,
    bottom: Math.min(bottom, window.innerHeight - VIEWPORT_MARGIN),
  }
}

/** 提取选区落在可见区域内的首尾片段矩形，支持跨屏长选区随滚动重新定位 */
const getVisibleSelectionRects = (
  range: Range,
  visTop: number,
  visBottom: number
): { first: DOMRect; last: DOMRect } | null => {
  const bounding = range.getBoundingClientRect()
  if (
    (bounding.width === 0 && bounding.height === 0) ||
    bounding.bottom <= visTop ||
    bounding.top >= visBottom
  ) {
    return null
  }

  const rects = range.getClientRects()
  let first: DOMRect | null = null
  let last: DOMRect | null = null
  for (let i = 0; i < rects.length; i++) {
    const rect = rects[i]
    if ((rect.width === 0 && rect.height === 0) || rect.bottom <= visTop || rect.top >= visBottom) {
      continue
    }
    if (!first || rect.top < first.top) {
      first = rect
    }
    if (!last || rect.bottom > last.bottom) {
      last = rect
    }
  }

  if (first && last) {
    return { first, last }
  }
  return { first: bounding, last: bounding }
}

/**
 * 监听消息列表内的文本选中，在可见选区的合适位置展示「引用文本」胶囊按钮。
 * 支持跨屏长选区：随页面滚动动态贴合可见选区上方、下方或顶部安全区，方便随时点击。
 */
export const SelectionQuotePopover = ({
  sessionId,
  containerRef,
  scrollRef,
}: {
  sessionId: string
  containerRef: RefObject<HTMLElement | null>
  scrollRef: RefObject<HTMLElement | null>
}) => {
  const t = useTranslate()
  const [popover, setPopover] = useState<PopoverState | null>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const draggingRef = useRef(false)

  const computePopover = useCallback((): PopoverState | null => {
    const container = containerRef.current
    if (!container) {
      return null
    }
    const selection = window.getSelection()
    if (!selection || selection.isCollapsed || selection.rangeCount === 0) {
      return null
    }
    const text = selection.toString().trim()
    if (!text) {
      return null
    }
    const range = selection.getRangeAt(0)
    if (
      !container.contains(range.startContainer) ||
      !container.contains(range.endContainer) ||
      isExcludedNode(range.startContainer) ||
      isExcludedNode(range.endContainer)
    ) {
      return null
    }

    const { top: visTop, bottom: visBottom } = getVisibleBounds(scrollRef.current)
    if (visBottom - visTop < POPOVER_HEIGHT) {
      return null
    }

    const visibleRects = getVisibleSelectionRects(range, visTop, visBottom)
    if (!visibleRects) {
      return null
    }

    const { first, last } = visibleRects
    const aboveTop = first.top - POPOVER_HEIGHT - VERTICAL_GAP
    const belowTop = last.bottom + VERTICAL_GAP

    let top: number
    let anchorRect: DOMRect
    if (aboveTop >= visTop) {
      // 优先贴在可见选区正上方
      top = aboveTop
      anchorRect = first
    } else if (belowTop + POPOVER_HEIGHT <= visBottom) {
      // 上方空间不足时（如选区顶部已滚出视野），贴在可见选区正下方
      top = belowTop
      anchorRect = last
    } else {
      // 选区同时跨越视口上下边缘时，固定在可见区域顶部安全区，确保随时可点
      top = visTop + VERTICAL_GAP
      anchorRect = first
    }

    const halfWidth = buttonRef.current
      ? buttonRef.current.offsetWidth / 2
      : POPOVER_ESTIMATED_HALF_WIDTH
    const rawLeft = anchorRect.left + anchorRect.width / 2
    const minLeft = halfWidth + VIEWPORT_MARGIN
    const maxLeft = Math.max(minLeft, window.innerWidth - halfWidth - VIEWPORT_MARGIN)
    const left = Math.min(Math.max(rawLeft, minLeft), maxLeft)
    return { text, top, left }
  }, [containerRef, scrollRef])

  // 切换会话时收起浮层
  useEffect(() => {
    setPopover(null)
  }, [sessionId])

  useEffect(() => {
    const onMouseDown = (event: MouseEvent) => {
      if (buttonRef.current?.contains(event.target as Node)) {
        return
      }
      draggingRef.current = true
      setPopover(null)
    }

    const onMouseUp = (event: MouseEvent) => {
      if (buttonRef.current?.contains(event.target as Node)) {
        return
      }
      draggingRef.current = false
      requestAnimationFrame(() => {
        setPopover(computePopover())
      })
    }

    const onSelectionChange = () => {
      if (draggingRef.current) {
        return
      }
      const selection = window.getSelection()
      if (!selection || selection.isCollapsed) {
        setPopover(null)
        return
      }
      setPopover(computePopover())
    }

    const onKeyUp = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setPopover(null)
        return
      }
      if (event.shiftKey || event.key === 'a' || event.key === 'A') {
        setPopover(computePopover())
      }
    }

    const onScrollOrResize = () => {
      if (draggingRef.current) {
        return
      }
      setPopover(computePopover())
    }

    document.addEventListener('mousedown', onMouseDown)
    document.addEventListener('mouseup', onMouseUp)
    document.addEventListener('selectionchange', onSelectionChange)
    document.addEventListener('keyup', onKeyUp)
    window.addEventListener('scroll', onScrollOrResize, true)
    window.addEventListener('resize', onScrollOrResize)

    return () => {
      document.removeEventListener('mousedown', onMouseDown)
      document.removeEventListener('mouseup', onMouseUp)
      document.removeEventListener('selectionchange', onSelectionChange)
      document.removeEventListener('keyup', onKeyUp)
      window.removeEventListener('scroll', onScrollOrResize, true)
      window.removeEventListener('resize', onScrollOrResize)
    }
  }, [computePopover])

  if (!popover) {
    return null
  }

  return createPortal(
    <button
      ref={buttonRef}
      type="button"
      className={styles.popover}
      style={{ top: popover.top, left: popover.left }}
      onMouseDown={(event) => {
        // 阻止点击按钮时先清空选区或触发失焦抖动
        event.preventDefault()
      }}
      onClick={() => {
        const quoted = popover.text
        window.getSelection()?.removeAllRanges()
        setPopover(null)
        requestInsertQuote(sessionId, quoted)
      }}
    >
      {t('quoteText')}
    </button>,
    document.body
  )
}
