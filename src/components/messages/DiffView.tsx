import clsx from 'clsx'
import { memo, useMemo } from 'react'
import { useTranslate } from '../../i18n/index.js'
import type { ToolFileDiff } from '../../services/chat/types.js'
import styles from './DiffView.module.css'

type DiffLine = {
  kind: 'context' | 'added' | 'removed'
  text: string
}

const MAX_LCS_LINES = 400
const MAX_DISPLAY_LINES = 240
const CONTEXT_RADIUS = 3

const splitLines = (text: string): string[] => {
  if (!text) {
    return []
  }
  const normalized = text.replace(/\r\n/g, '\n')
  const lines = normalized.split('\n')
  if (lines.at(-1) === '') {
    lines.pop()
  }
  return lines
}

/** 轻量级行级 Diff：小文件走精确 LCS，大文件回退前后缀裁剪，避免阻塞主线程 */
const computeLineDiff = (oldText: string, newText: string): DiffLine[] => {
  const oldLines = splitLines(oldText)
  const newLines = splitLines(newText)

  if (oldLines.length === 0) {
    return newLines.map((text) => ({ kind: 'added', text }))
  }
  if (newLines.length === 0) {
    return oldLines.map((text) => ({ kind: 'removed', text }))
  }

  let prefix = 0
  while (
    prefix < oldLines.length &&
    prefix < newLines.length &&
    oldLines[prefix] === newLines[prefix]
  ) {
    prefix += 1
  }

  let suffix = 0
  while (
    suffix < oldLines.length - prefix &&
    suffix < newLines.length - prefix &&
    oldLines[oldLines.length - 1 - suffix] === newLines[newLines.length - 1 - suffix]
  ) {
    suffix += 1
  }

  const oldMid = oldLines.slice(prefix, oldLines.length - suffix)
  const newMid = newLines.slice(prefix, newLines.length - suffix)
  const prefixContext = oldLines
    .slice(Math.max(0, prefix - CONTEXT_RADIUS), prefix)
    .map((text): DiffLine => ({ kind: 'context', text }))
  const suffixContext = oldLines
    .slice(oldLines.length - suffix, Math.min(oldLines.length, oldLines.length - suffix + CONTEXT_RADIUS))
    .map((text): DiffLine => ({ kind: 'context', text }))

  if (oldMid.length === 0 && newMid.length === 0) {
    return []
  }

  if (oldMid.length > MAX_LCS_LINES || newMid.length > MAX_LCS_LINES) {
    return [
      ...prefixContext,
      ...oldMid.map((text): DiffLine => ({ kind: 'removed', text })),
      ...newMid.map((text): DiffLine => ({ kind: 'added', text })),
      ...suffixContext,
    ]
  }

  const rows = oldMid.length + 1
  const cols = newMid.length + 1
  const dp = new Int32Array(rows * cols)
  for (let i = oldMid.length - 1; i >= 0; i -= 1) {
    for (let j = newMid.length - 1; j >= 0; j -= 1) {
      dp[i * cols + j] =
        oldMid[i] === newMid[j]
          ? dp[(i + 1) * cols + (j + 1)] + 1
          : Math.max(dp[(i + 1) * cols + j], dp[i * cols + (j + 1)])
    }
  }

  const mid: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < oldMid.length && j < newMid.length) {
    if (oldMid[i] === newMid[j]) {
      mid.push({ kind: 'context', text: oldMid[i] })
      i += 1
      j += 1
    } else if (dp[(i + 1) * cols + j] >= dp[i * cols + (j + 1)]) {
      mid.push({ kind: 'removed', text: oldMid[i] })
      i += 1
    } else {
      mid.push({ kind: 'added', text: newMid[j] })
      j += 1
    }
  }
  while (i < oldMid.length) {
    mid.push({ kind: 'removed', text: oldMid[i] })
    i += 1
  }
  while (j < newMid.length) {
    mid.push({ kind: 'added', text: newMid[j] })
    j += 1
  }

  return [...prefixContext, ...mid, ...suffixContext]
}

export const DiffView = memo(function DiffView({ diff }: { diff: ToolFileDiff }) {
  const t = useTranslate()
  const { lines, added, removed, truncatedCount } = useMemo(() => {
    const all = computeLineDiff(diff.oldText, diff.newText)
    let addedCount = 0
    let removedCount = 0
    for (const line of all) {
      if (line.kind === 'added') {
        addedCount += 1
      } else if (line.kind === 'removed') {
        removedCount += 1
      }
    }
    return {
      lines: all.slice(0, MAX_DISPLAY_LINES),
      added: addedCount,
      removed: removedCount,
      truncatedCount: Math.max(0, all.length - MAX_DISPLAY_LINES),
    }
  }, [diff.oldText, diff.newText])

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <span className={styles.filePath} title={diff.path}>
          {diff.path}
        </span>
        <span className={styles.stats}>
          {added > 0 ? <span className={styles.addedStat}>+{added}</span> : null}
          {removed > 0 ? <span className={styles.removedStat}>-{removed}</span> : null}
        </span>
      </div>
      <div className={styles.body}>
        {lines.map((line, index) => (
          <div key={index} className={clsx(styles.line, styles[line.kind])}>
            <span className={styles.sign}>
              {line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : ' '}
            </span>
            <span className={styles.code}>{line.text || ' '}</span>
          </div>
        ))}
      </div>
      {truncatedCount > 0 ? (
        <div className={styles.truncated}>
          {t('toolDiffTruncated', { count: truncatedCount })}
        </div>
      ) : null}
    </div>
  )
})
