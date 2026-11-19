import { useIsMobile } from '../../app/viewport.js'
import { useTranslate } from '../../i18n/index.js'
import { VelaBrandMark } from '../base/icons.js'
import { ChatInput } from '../chatInput/ChatInput.js'
import { MobileHeader } from './MobileHeader.js'
import styles from './Welcome.module.css'

/** 新对话：品牌标志 + 问候语 + 副标题 + 输入框。桌面端居中；移动端输入框吸底 */
export const Welcome = () => {
  const t = useTranslate()
  const isMobile = useIsMobile()
  return (
    <div className={styles.container}>
      {isMobile ? <MobileHeader /> : <div className={styles.dragBar} />}
      <div className={styles.welcome}>
        <div className={styles.hero}>
          <div className={styles.brandBadge}>
            <VelaBrandMark size={28} />
          </div>
          <div className={styles.greeting}>{t('welcomeMessageChat250808')}</div>
          <div className={styles.subtitle}>{t('welcomeSubtitle')}</div>
        </div>
        <ChatInput sessionId={null} />
        {isMobile ? <div className={styles.caveat}>{t('contentCaveat')}</div> : null}
      </div>
    </div>
  )
}
