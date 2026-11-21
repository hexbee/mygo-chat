import { useState } from 'react'
import { useTranslate } from '../../i18n/index.js'
import {
  resetApprovalBlacklist,
  setApprovalBlacklist,
  useSettings,
} from '../../services/settings.js'
import { Button } from '../base/Button.js'
import { showToast } from '../base/toastState.js'
import styles from './CommandBlacklistSetting.module.css'

export const CommandBlacklistSetting = () => {
  const t = useTranslate()
  const rules = useSettings((state) => state.approvalBlacklist)
  const [draft, setDraft] = useState('')

  const handleAdd = () => {
    const entries = draft
      .split(/[\n,]+/)
      .map((item) => item.trim())
      .filter(Boolean)
    if (entries.length === 0) {
      return
    }
    setApprovalBlacklist([...rules, ...entries])
    setDraft('')
  }

  const handleRemove = (target: string) => {
    setApprovalBlacklist(rules.filter((item) => item !== target))
  }

  const handleReset = () => {
    resetApprovalBlacklist()
    showToast(t('settingsBlacklistResetToast'))
  }

  return (
    <div className={styles.section}>
      <div className={styles.headerRow}>
        <div className={styles.headerText}>
          <div className={styles.title}>{t('settingsBlacklistTitle')}</div>
          <div className={styles.desc}>{t('settingsBlacklistDescription')}</div>
        </div>
        <Button variant="outlined" size="m" className={styles.actionBtn} onClick={handleReset}>
          {t('settingsBlacklistReset')}
        </Button>
      </div>

      <div className={styles.addRow}>
        <input
          type="text"
          className={styles.input}
          value={draft}
          placeholder={t('settingsBlacklistPlaceholder')}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              handleAdd()
            }
          }}
        />
        <Button
          variant="outlined"
          size="m"
          className={styles.actionBtn}
          disabled={!draft.trim()}
          onClick={handleAdd}
        >
          {t('settingsBlacklistAdd')}
        </Button>
      </div>

      {rules.length > 0 ? (
        <div className={styles.tags}>
          {rules.map((rule) => (
            <span key={rule} className={styles.tag}>
              <span className={styles.tagText}>{rule}</span>
              <button
                type="button"
                className={styles.tagRemove}
                aria-label={`${t('delete')} ${rule}`}
                onClick={() => handleRemove(rule)}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      ) : (
        <div className={styles.empty}>{t('settingsBlacklistEmpty')}</div>
      )}
    </div>
  )
}
