import { memo } from 'react'
import { useTranslate } from '../../i18n/index.js'
import {
  approveToolCall,
  rejectToolCall,
  type PendingApproval,
} from '../../services/chat/store.js'
import styles from './ApprovalCard.module.css'

const extractHighlightTarget = (approval: PendingApproval): string => {
  const str = (key: string) =>
    typeof approval.input[key] === 'string' ? (approval.input[key] as string) : ''
  if (approval.toolName === 'bash') {
    return str('command')
  }
  return str('file_path') || str('path') || JSON.stringify(approval.input)
}

export const ApprovalCard = memo(function ApprovalCard({
  approval,
}: {
  approval: PendingApproval
}) {
  const t = useTranslate()
  const target = extractHighlightTarget(approval)
  const description =
    typeof approval.input.description === 'string' ? approval.input.description : ''
  const effectiveWorkdir =
    (typeof approval.input.workdir === 'string' && approval.input.workdir) || approval.workdir

  return (
    <div className={styles.card} role="region" aria-label={t('toolApprovalTitle')}>
      <div className={styles.header}>
        <span className={styles.badge}>{approval.toolName}</span>
        <span className={styles.title}>{t('toolApprovalTitle')}</span>
      </div>
      {description ? <div className={styles.reason}>{description}</div> : null}
      <div className={styles.reason}>{approval.reason}</div>
      {target ? <div className={styles.detail}>{target}</div> : null}
      {effectiveWorkdir ? (
        <div className={styles.metaRow}>
          <span>{t('toolApprovalWorkdir')}:</span>
          <span className={styles.metaValue}>{effectiveWorkdir}</span>
        </div>
      ) : null}
      <div className={styles.actions}>
        <button
          type="button"
          className={styles.allowBtn}
          onClick={() => approveToolCall(approval.toolUseId)}
        >
          {t('toolApprovalAllowOnce')}
        </button>
        <button
          type="button"
          className={styles.rejectBtn}
          onClick={() => rejectToolCall(approval.toolUseId)}
        >
          {t('toolApprovalReject')}
        </button>
      </div>
    </div>
  )
})
