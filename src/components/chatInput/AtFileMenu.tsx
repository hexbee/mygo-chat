import clsx from 'clsx'
import { Fragment, memo, useEffect, useRef } from 'react'
import { useTranslate } from '../../i18n/index.js'
import type { WorkspaceFileCandidate } from '../../mygo.js'
import styles from './AtFileMenu.module.css'

export type AtBreadcrumbItem = {
  label: string
  /** Relative directory path with trailing slash, or empty string for workspace root. */
  path: string
  current?: boolean
}

export const FolderMentionIcon = () => (
  <svg width={15} height={15} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path
      d="M2.2 4.3C2.2 3.47 2.87 2.8 3.7 2.8H6.15C6.57 2.8 6.97 2.98 7.25 3.3L8.05 4.2H12.3C13.13 4.2 13.8 4.87 13.8 5.7V11.7C13.8 12.53 13.13 13.2 12.3 13.2H3.7C2.87 13.2 2.2 12.53 2.2 11.7V4.3Z"
      stroke="currentColor"
      strokeWidth={1.3}
      strokeLinejoin="round"
    />
  </svg>
)

export const FileMentionIcon = () => (
  <svg width={15} height={15} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <rect
      x={2.4}
      y={2.4}
      width={11.2}
      height={11.2}
      rx={2.2}
      stroke="currentColor"
      strokeWidth={1.3}
    />
    <path
      d="M5.2 6.2H10.8M5.2 9.4H8.6"
      stroke="currentColor"
      strokeWidth={1.3}
      strokeLinecap="round"
    />
  </svg>
)

const ChevronRightIcon = () => (
  <svg width={12} height={12} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path
      d="M6.2 3.8L10.4 8L6.2 12.2"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
)

const splitPath = (relPath: string): { name: string; parent: string } => {
  const slash = relPath.lastIndexOf('/')
  if (slash < 0) {
    return { name: relPath, parent: '' }
  }
  return {
    name: relPath.slice(slash + 1),
    parent: relPath.slice(0, slash),
  }
}

export const AtFileMenu = memo(function AtFileMenu({
  open,
  items,
  crumbs,
  selectedIndex,
  hasWorkspace,
  onSelect,
  onDrill,
  onCrumb,
  onHover,
}: {
  open: boolean
  items: WorkspaceFileCandidate[]
  crumbs: AtBreadcrumbItem[]
  selectedIndex: number
  hasWorkspace: boolean
  onSelect: (item: WorkspaceFileCandidate) => void
  onDrill: (item: WorkspaceFileCandidate) => void
  onCrumb: (path: string) => void
  onHover: (index: number) => void
}) {
  const t = useTranslate()
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([])

  useEffect(() => {
    if (!open) {
      return
    }
    itemRefs.current[selectedIndex]?.scrollIntoView({ block: 'nearest' })
  }, [open, selectedIndex, items.length])

  if (!open) {
    return null
  }

  if (!hasWorkspace) {
    return (
      <div className={styles.menu} role="listbox" aria-label={t('atFileMenuSectionTitle')}>
        <div className={styles.empty}>{t('atFileMenuNoWorkspace')}</div>
      </div>
    )
  }

  if (items.length === 0 && crumbs.length === 0) {
    return null
  }

  const showParentPath = crumbs.length === 0

  return (
    <div className={styles.menu}>
      {crumbs.length > 0 ? (
        <nav className={styles.crumbs} aria-label={t('atFileMenuCrumbRoot')}>
          {crumbs.map((crumb, idx) => (
            <Fragment key={`${idx}:${crumb.path}`}>
              {idx > 0 ? (
                <span className={styles.crumbSep} aria-hidden="true">
                  <ChevronRightIcon />
                </span>
              ) : null}
              <button
                type="button"
                className={clsx(styles.crumb, crumb.current && styles.crumbCurrent)}
                disabled={crumb.current}
                aria-current={crumb.current ? 'location' : undefined}
                onMouseDown={(event) => {
                  event.preventDefault()
                  if (!crumb.current) {
                    onCrumb(crumb.path)
                  }
                }}
              >
                {crumb.label}
              </button>
            </Fragment>
          ))}
        </nav>
      ) : null}

      <div className={styles.viewport} role="listbox" aria-label={t('atFileMenuSectionTitle')}>
        <div className={styles.header}>{t('atFileMenuSectionTitle')}</div>
        <div className={styles.list}>
          {items.map((item, index) => {
            const isDir = item.kind === 'directory'
            const isSelected = index === selectedIndex
            const { name, parent } = splitPath(item.path)
            return (
              <button
                key={`${item.kind}:${item.path}`}
                ref={(el) => {
                  itemRefs.current[index] = el
                }}
                type="button"
                role="option"
                aria-selected={isSelected}
                className={clsx(styles.item, isSelected && styles.itemSelected)}
                onMouseMove={isSelected ? undefined : () => onHover(index)}
                onMouseDown={(event) => {
                  event.preventDefault()
                  onSelect(item)
                }}
              >
                <span className={styles.icon}>
                  {isDir ? <FolderMentionIcon /> : <FileMentionIcon />}
                </span>
                <span className={styles.name}>{isDir ? `${name}/` : name}</span>
                <span className={styles.meta}>
                  {showParentPath && parent ? (
                    <span className={styles.parentPath}>{parent}</span>
                  ) : null}
                  {isDir ? (
                    <span className={styles.drillGroup}>
                      <span className={styles.drillHint} aria-hidden="true">
                        <span>{t('atFileMenuDrillHint')}</span>
                        <kbd className={styles.drillKey}>{t('atFileMenuDrillKey')}</kbd>
                      </span>
                      <span
                        role="button"
                        aria-label={t('atFileMenuDrillHint')}
                        className={styles.drillChevron}
                        onMouseDown={(event) => {
                          event.preventDefault()
                          event.stopPropagation()
                          onDrill(item)
                        }}
                      >
                        <ChevronRightIcon />
                      </span>
                    </span>
                  ) : null}
                </span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
})
