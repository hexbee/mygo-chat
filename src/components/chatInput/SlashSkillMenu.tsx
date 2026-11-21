import clsx from 'clsx'
import { memo, type ReactNode } from 'react'
import { useTranslate } from '../../i18n/index.js'
import type { SkillSummary } from '../../mygo.js'
import styles from './SlashSkillMenu.module.css'

export type SlashCommandItem = {
  name: string
  description: string
}

export type SlashMenuItem =
  | { kind: 'skill'; skill: SkillSummary }
  | { kind: 'command'; command: SlashCommandItem }

export const SkillSparkleIcon = () => (
  <svg width={15} height={15} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path
      d="M6.8 1.8L8.05 5.45L11.7 6.7L8.05 7.95L6.8 11.6L5.55 7.95L1.9 6.7L5.55 5.45L6.8 1.8Z"
      stroke="currentColor"
      strokeWidth={1.3}
      strokeLinejoin="round"
    />
    <path
      d="M12.3 9.7L12.9 11.4L14.6 12L12.9 12.6L12.3 14.3L11.7 12.6L10 12L11.7 11.4L12.3 9.7Z"
      fill="currentColor"
    />
  </svg>
)

export const CommandSlashIcon = () => (
  <svg width={15} height={15} viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <rect x={2} y={2} width={12} height={12} rx={3.5} stroke="currentColor" strokeWidth={1.3} />
    <path d="M9.6 4.7L6.4 11.3" stroke="currentColor" strokeWidth={1.3} strokeLinecap="round" />
  </svg>
)

const highlightText = (text: string, query: string): ReactNode => {
  if (!query) {
    return text
  }
  const lower = text.toLowerCase()
  const idx = lower.indexOf(query)
  if (idx < 0) {
    return text
  }
  return (
    <>
      {text.slice(0, idx)}
      <span className={styles.highlight}>{text.slice(idx, idx + query.length)}</span>
      {text.slice(idx + query.length)}
    </>
  )
}

export const SlashSkillMenu = memo(function SlashSkillMenu({
  skills,
  commands,
  query,
  selectedIndex,
  onSelect,
  onHover,
}: {
  skills: SkillSummary[]
  commands: SlashCommandItem[]
  query: string
  selectedIndex: number
  onSelect: (item: SlashMenuItem) => void
  onHover: (index: number) => void
}) {
  const t = useTranslate()

  if (skills.length === 0 && commands.length === 0) {
    return null
  }

  return (
    <div className={styles.menu} role="listbox" aria-label={t('skillSlashTitle')}>
      {skills.length > 0 ? (
        <div className={styles.section}>
          <div className={styles.header}>
            {t('skillMenuSkillsSection', { count: skills.length })}
          </div>
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
                  onSelect({ kind: 'skill', skill })
                }}
              >
                <span className={styles.icon}>
                  <SkillSparkleIcon />
                </span>
                <span className={styles.name}>{highlightText(skill.name, query)}</span>
                {skill.description ? (
                  <span className={styles.desc}>{highlightText(skill.description, query)}</span>
                ) : null}
              </button>
            ))}
          </div>
        </div>
      ) : null}

      {skills.length > 0 && commands.length > 0 ? <div className={styles.divider} /> : null}

      {commands.length > 0 ? (
        <div className={styles.section}>
          <div className={styles.header}>
            {t('skillMenuCommandsSection', { count: commands.length })}
          </div>
          <div className={styles.list}>
            {commands.map((command, idx) => {
              const globalIndex = skills.length + idx
              return (
                <button
                  key={command.name}
                  type="button"
                  role="option"
                  aria-selected={globalIndex === selectedIndex}
                  className={clsx(
                    styles.item,
                    globalIndex === selectedIndex && styles.itemSelected
                  )}
                  onMouseEnter={() => onHover(globalIndex)}
                  onMouseDown={(event) => {
                    event.preventDefault()
                    onSelect({ kind: 'command', command })
                  }}
                >
                  <span className={styles.icon}>
                    <CommandSlashIcon />
                  </span>
                  <span className={styles.name}>/{highlightText(command.name, query)}</span>
                  {command.description ? (
                    <span className={styles.desc}>{highlightText(command.description, query)}</span>
                  ) : null}
                </button>
              )
            })}
          </div>
        </div>
      ) : null}
    </div>
  )
})
