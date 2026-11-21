import clsx from 'clsx'
import { memo } from 'react'
import { useTranslate } from '../../i18n/index.js'
import type { SkillSummary } from '../../mygo.js'
import styles from './SlashSkillMenu.module.css'

export const SlashSkillMenu = memo(function SlashSkillMenu({
  skills,
  selectedIndex,
  onSelect,
  onHover,
}: {
  skills: SkillSummary[]
  selectedIndex: number
  onSelect: (skill: SkillSummary) => void
  onHover: (index: number) => void
}) {
  const t = useTranslate()

  if (skills.length === 0) {
    return null
  }

  return (
    <div className={styles.menu} role="listbox" aria-label={t('skillSlashTitle')}>
      <div className={styles.header}>{t('skillSlashTitle')}</div>
      <div className={styles.list}>
        {skills.map((skill, index) => (
          <button
            key={`${skill.scope}:${skill.name}`}
            type="button"
            role="option"
            aria-selected={index === selectedIndex}
            className={clsx(styles.item, index === selectedIndex && styles.itemSelected)}
            onMouseEnter={() => onHover(index)}
            onMouseDown={(event) => {
              event.preventDefault()
              onSelect(skill)
            }}
          >
            <div className={styles.itemTop}>
              <span className={styles.name}>/{skill.name}</span>
              <span className={styles.scope}>
                {skill.scope === 'project' ? t('skillScopeProject') : t('skillScopeUser')}
              </span>
            </div>
            {skill.description ? <div className={styles.desc}>{skill.description}</div> : null}
          </button>
        ))}
      </div>
    </div>
  )
})
