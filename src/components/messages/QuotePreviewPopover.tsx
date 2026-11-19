import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import styles from './QuotePreviewPopover.module.css'

type PreviewState = {
  target: HTMLElement
  text: string
  top: number
  left: number
  ready: boolean
}

const GAP = 8
const VIEWPORT_MARGIN = 12
const HIDE_DELAY = 120

/**
 * 全局引用文本悬浮预览浮层：
 * 监听输入框与消息气泡中带有 data-quote-preview="true" 的引用节点，
 * 鼠标悬浮时展示完整原文（保留换行、限高可滚动），移开后自动消失。
 */
export const QuotePreviewPopover = () => {
  const [preview, setPreview] = useState<PreviewState | null>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const activeTargetRef = useRef<HTMLElement | null>(null)
  const hideTimerRef = useRef<number | null>(null)

  const cancelHide = useCallback(() => {
    if (hideTimerRef.current !== null) {
      window.clearTimeout(hideTimerRef.current)
      hideTimerRef.current = null
    }
  }, [])

  const clearActiveTarget = useCallback(() => {
    if (activeTargetRef.current) {
      delete activeTargetRef.current.dataset.quotePreviewing
      activeTargetRef.current = null
    }
  }, [])

  const hideNow = useCallback(() => {
    cancelHide()
    clearActiveTarget()
    setPreview(null)
  }, [cancelHide, clearActiveTarget])

  const scheduleHide = useCallback(() => {
    cancelHide()
    hideTimerRef.current = window.setTimeout(() => {
      hideTimerRef.current = null
      clearActiveTarget()
      setPreview(null)
    }, HIDE_DELAY)
  }, [cancelHide, clearActiveTarget])

  const showForTarget = useCallback(
    (target: HTMLElement) => {
      const text = target.dataset.quoteText
      if (!text) {
        return
      }
      cancelHide()
      if (activeTargetRef.current === target) {
        return
      }
      clearActiveTarget()
      activeTargetRef.current = target
      target.dataset.quotePreviewing = 'true'
      const rect = target.getBoundingClientRect()
      setPreview({
        target,
        text,
        top: rect.top - GAP,
        left: rect.left,
        ready: false,
      })
    },
    [cancelHide, clearActiveTarget]
  )

  useLayoutEffect(() => {
    if (!preview) {
      return
    }
    const popover = popoverRef.current
    if (!popover) {
      return
    }
    if (!preview.target.isConnected) {
      hideNow()
      return
    }
    const rect = preview.target.getBoundingClientRect()
    const width = popover.offsetWidth
    const height = popover.offsetHeight
    const maxLeft = Math.max(VIEWPORT_MARGIN, window.innerWidth - width - VIEWPORT_MARGIN)
    const left = Math.min(Math.max(rect.left, VIEWPORT_MARGIN), maxLeft)
    const aboveTop = rect.top - height - GAP
    const belowTop = rect.bottom + GAP
    let top: number
    if (aboveTop >= VIEWPORT_MARGIN) {
      top = aboveTop
    } else if (belowTop + height <= window.innerHeight - VIEWPORT_MARGIN) {
      top = belowTop
    } else {
      top = Math.max(VIEWPORT_MARGIN, Math.min(aboveTop, window.innerHeight - height - VIEWPORT_MARGIN))
    }
    if (!preview.ready || preview.top !== top || preview.left !== left) {
      setPreview((current) =>
        current && current.target === preview.target
          ? { ...current, top, left, ready: true }
          : current
      )
    }
  }, [preview, hideNow])

  useEffect(() => {
    const onMouseOver = (event: MouseEvent) => {
      const target = (event.target as Element | null)?.closest<HTMLElement>(
        '[data-quote-preview="true"]'
      )
      if (target) {
        showForTarget(target)
      }
    }

    const onMouseOut = (event: MouseEvent) => {
      const fromTarget = (event.target as Element | null)?.closest<HTMLElement>(
        '[data-quote-preview="true"]'
      )
      if (!fromTarget || fromTarget !== activeTargetRef.current) {
        return
      }
      const next = event.relatedTarget as Node | null
      if (next && (fromTarget.contains(next) || popoverRef.current?.contains(next))) {
        return
      }
      scheduleHide()
    }

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        hideNow()
      }
    }

    const onMouseDown = (event: MouseEvent) => {
      const node = event.target as Node | null
      if (node && popoverRef.current?.contains(node)) {
        return
      }
      hideNow()
    }

    const onScroll = (event: Event) => {
      const node = event.target as Node | null
      if (node && popoverRef.current?.contains(node)) {
        return
      }
      if (activeTargetRef.current) {
        hideNow()
      }
    }

    document.addEventListener('mouseover', onMouseOver)
    document.addEventListener('mouseout', onMouseOut)
    document.addEventListener('keydown', onKeyDown)
    document.addEventListener('mousedown', onMouseDown)
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('resize', hideNow)

    return () => {
      cancelHide()
      document.removeEventListener('mouseover', onMouseOver)
      document.removeEventListener('mouseout', onMouseOut)
      document.removeEventListener('keydown', onKeyDown)
      document.removeEventListener('mousedown', onMouseDown)
      window.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('resize', hideNow)
    }
  }, [showForTarget, scheduleHide, hideNow, cancelHide])

  if (!preview) {
    return null
  }

  return createPortal(
    <div
      ref={popoverRef}
      role="tooltip"
      className={styles.popover}
      style={{
        top: preview.top,
        left: preview.left,
        visibility: preview.ready ? 'visible' : 'hidden',
      }}
      onMouseEnter={cancelHide}
      onMouseLeave={(event) => {
        const next = event.relatedTarget as Node | null
        if (next && activeTargetRef.current?.contains(next)) {
          return
        }
        hideNow()
      }}
    >
      {preview.text}
    </div>,
    document.body
  )
}
