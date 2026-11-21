import clsx from 'clsx'
import { memo, useState } from 'react'
import { useTranslate } from '../../i18n/index.js'
import {
  basenameOfPath,
  openWorkspacePath,
  revealWorkspaceInFinder,
  saveWorkspacePathAs,
} from '../../services/workspace/store.js'
import { showToast } from '../base/toastState.js'
import { FilePreviewModal } from './FilePreviewModal.js'
import styles from './ProducedFilesBar.module.css'

const fileBadge = (filePath: string): string => {
  const name = basenameOfPath(filePath)
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) {
    return 'FILE'
  }
  return name.slice(dot + 1, dot + 6).toUpperCase()
}

export const ProducedFilesBar = memo(function ProducedFilesBar({
  files,
  title,
  compact = false,
}: {
  files: string[]
  title?: string
  compact?: boolean
}) {
  const t = useTranslate()
  const [previewPath, setPreviewPath] = useState<string | null>(null)

  if (files.length === 0) {
    return null
  }

  const handleSaveAs = async (filePath: string) => {
    try {
      const saved = await saveWorkspacePathAs(filePath)
      if (saved) {
        showToast(t('toolSaveAsSuccess', { path: saved }))
      }
    } catch {
      showToast(t('toolSaveAsFailed'))
    }
  }

  return (
    <>
      <div className={clsx(styles.container, compact && styles.compact)}>
        {title ? <div className={styles.title}>{title}</div> : null}
        <div className={styles.list}>
          {files.map((filePath) => (
            <div key={filePath} className={styles.chip} title={filePath}>
              <span className={styles.badge}>{fileBadge(filePath)}</span>
              <span className={styles.name}>{basenameOfPath(filePath)}</span>
              <div className={styles.actions}>
                <button
                  type="button"
                  className={styles.btn}
                  onClick={() => setPreviewPath(filePath)}
                >
                  {t('toolPreviewFile')}
                </button>
                <button
                  type="button"
                  className={styles.btn}
                  onClick={() => {
                    void openWorkspacePath(filePath)
                  }}
                >
                  {t('toolOpenFile')}
                </button>
                <button
                  type="button"
                  className={styles.btn}
                  onClick={() => {
                    void handleSaveAs(filePath)
                  }}
                >
                  {t('toolSaveAs')}
                </button>
                <button
                  type="button"
                  className={styles.btn}
                  onClick={() => {
                    void revealWorkspaceInFinder(filePath)
                  }}
                >
                  {t('workspaceRevealInFinder')}
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>
      {previewPath ? (
        <FilePreviewModal filePath={previewPath} onClose={() => setPreviewPath(null)} />
      ) : null}
    </>
  )
})
