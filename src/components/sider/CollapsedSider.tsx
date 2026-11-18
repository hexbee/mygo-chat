import { IconPanelLeftOutline16, NewChatIcon } from '../base/icons.js'
import { useTranslate } from '../../i18n/index.js'
import { setSiderCollapsed } from '../../services/settings.js'
import { newChat } from '../../services/chat/store.js'
import { IconButton } from '../base/IconButton.js'
import { DesktopUpdateBadge } from '../updater/DesktopUpdateIndicator.js'
import { modKey } from '../../app/shortcuts.js'
import styles from './Sider.module.css'

/** 收起后左上角的按钮组 */
export const CollapsedSider = () => {
  const t = useTranslate()
  return (
    <div className={styles.collapsed}>
      <div className={styles.collapsedGroup}>
        <div style={{ position: 'relative', display: 'inline-flex' }}>
          <IconButton
            size="m"
            tone="primary"
            icon={<IconPanelLeftOutline16 />}
            tooltip={`${t('expandSiderbarButton')} (${modKey}B)`}
            onClick={() => setSiderCollapsed(false)}
          />
          <DesktopUpdateBadge />
        </div>
        <IconButton
          size="m"
          tone="primary"
          icon={<NewChatIcon />}
          tooltip={`${t('newChat')} (${modKey}N)`}
          onClick={newChat}
        />
      </div>
    </div>
  )
}
