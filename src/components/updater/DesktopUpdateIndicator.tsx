import { useTranslate } from '../../i18n/index.js'
import { IconDownloadOutline14, IconLoadingOutline14 } from '../base/icons.js'
import { Tooltip } from '../base/Tooltip.js'
import { useUpdateStore } from '../../services/updater/updateStore.js'
import styles from './DesktopUpdateIndicator.module.css'

export const DesktopUpdateIndicator = () => {
  const t = useTranslate()
  const state = useUpdateStore((s) => s.state)
  const openDialog = useUpdateStore((s) => s.openDialog)

  if (state.phase === 'idle') {
    return null
  }

  let label = ''
  let isBusy = false
  let isError = false
  let isProgress = false

  switch (state.phase) {
    case 'checking':
      label = t('desktopUpdateChecking')
      isBusy = true
      break
    case 'available':
      label = t('desktopUpdateAvailable')
      break
    case 'downloading':
      label = `${state.percent || 0}%`
      isProgress = true
      break
    case 'verifying':
      label = t('desktopUpdateVerifying')
      isBusy = true
      break
    case 'installing':
      label = t('desktopUpdateInstalling')
      isBusy = true
      break
    case 'ready':
      label = t('desktopUpdateReady')
      break
    case 'error':
      label = t('desktopUpdateRetry')
      isError = true
      break
    default:
      return null
  }

  const tooltipText =
    state.phase === 'downloading'
      ? t('desktopUpdateDownloading', { percent: state.percent })
      : state.phase === 'available' && state.version
        ? t('desktopUpdateNewVersion', { version: state.version })
        : label

  return (
    <Tooltip content={tooltipText} placement="top">
      <button
        type="button"
        className={styles.indicator}
        aria-label={label}
        aria-disabled={isBusy}
        onClick={() => {
          if (!isBusy) {
            openDialog()
          }
        }}
      >
        <span className={styles.icon} aria-hidden="true">
          {isError ? (
            <span className={styles.errorDot} />
          ) : isBusy ? (
            <IconLoadingOutline14 className={styles.spinner} size={14} />
          ) : (
            <IconDownloadOutline14 size={14} />
          )}
        </span>
        <span className={isProgress ? styles.progress : undefined}>{label}</span>
      </button>
    </Tooltip>
  )
}

/** 侧边栏折叠/顶栏菜单按钮上的更新红/蓝点小徽标 */
export const DesktopUpdateBadge = () => {
  const phase = useUpdateStore((s) => s.state.phase)
  if (phase === 'idle') {
    return null
  }
  return <span className={styles.badge} />
}
