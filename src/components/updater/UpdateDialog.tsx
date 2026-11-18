import { useTranslate } from '../../i18n/index.js'
import { Button } from '../base/Button.js'
import { Modal } from '../base/Modal.js'
import { showToast } from '../base/toastState.js'
import { Markdown } from '../markdown/Markdown.js'
import { useChatStore } from '../../services/chat/store.js'
import { useUpdateStore } from '../../services/updater/updateStore.js'
import styles from './UpdateDialog.module.css'

export const UpdateDialog = () => {
  const t = useTranslate()
  const { state, dialogOpen, busy, closeDialog, startDownload, relaunch } = useUpdateStore()

  if (!dialogOpen) {
    return null
  }

  const isDownloading = state.phase === 'downloading'
  const isReady = state.phase === 'ready'
  const isAvailable = state.phase === 'available'
  const isVerifying = state.phase === 'verifying'
  const isError = state.phase === 'error'

  const handleRelaunch = () => {
    // 借鉴 deepseek-harness 的任务感知保护：活跃对话进行中不强制重启
    const isStreaming = Object.keys(useChatStore.getState().streaming).length > 0
    if (isStreaming) {
      showToast(t('desktopUpdateStreamingBusy'))
      return
    }
    relaunch()
  }

  return (
    <Modal
      title={t('desktopUpdateTitle')}
      closable={!isDownloading && !isVerifying}
      onClose={closeDialog}
      className={styles.dialog}
      footer={
        <div className={styles.actions}>
          <Button variant="outlined" disabled={isDownloading || isVerifying} onClick={closeDialog}>
            {isReady ? t('desktopUpdateLater') : t('cancel')}
          </Button>

          {isAvailable && (
            <Button variant="primary" disabled={busy} onClick={() => startDownload()}>
              {t('desktopUpdateDownload')}
            </Button>
          )}

          {isDownloading && (
            <Button variant="primary" disabled>
              {t('desktopUpdateDownloading', { percent: state.percent })}
            </Button>
          )}

          {isVerifying && (
            <Button variant="primary" disabled>
              {t('desktopUpdateVerifying')}
            </Button>
          )}

          {isReady && (
            <Button variant="primary" onClick={handleRelaunch}>
              {t('desktopUpdateRelaunch')}
            </Button>
          )}

          {isError && (
            <Button variant="primary" disabled={busy} onClick={() => startDownload()}>
              {t('desktopUpdateRetry')}
            </Button>
          )}
        </div>
      }
    >
      <div className={styles.versionHeader}>
        <div className={styles.versionTitle}>
          {state.version ? t('desktopUpdateNewVersion', { version: state.version }) : t('desktopUpdateTitle')}
        </div>
      </div>

      {isDownloading && (
        <div className={styles.progressBarContainer}>
          <div className={styles.progressBar}>
            <div className={styles.progressFill} style={{ width: `${state.percent || 0}%` }} />
          </div>
          <div className={styles.progressLabel}>
            <span>{t('desktopUpdateDownloading', { percent: state.percent })}</span>
            <span>{state.percent || 0}%</span>
          </div>
        </div>
      )}

      {state.notes ? (
        <>
          <div className={styles.notesLabel}>{t('desktopUpdateNotes')}</div>
          <div className={styles.notesBox}>
            <Markdown
              content={state.notes}
              loading={false}
              cacheKey={`notes-${state.version}`}
              disableLink={false}
            />
          </div>
        </>
      ) : null}

      {isError && state.error ? (
        <div className={styles.errorMessage}>{state.error}</div>
      ) : null}
    </Modal>
  )
}
