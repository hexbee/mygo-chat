import clsx from 'clsx'
import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslate, type TranslationKey } from '../../i18n/index.js'
import type { ChatModel, ReasoningEffort } from '../../services/api/types.js'
import { setModel, setReasoningEffort, useSettings } from '../../services/settings.js'
import { useAnchoredPosition, useDismiss } from '../base/anchored.js'
import {
  IconCheckedOutline14,
  IconChevronDownOutline14,
  IconChevronRightOutline14,
} from '../base/icons.js'
import styles from './ComposerPickers.module.css'

type Pane = 'root' | 'model' | 'effort'

type ModelOption = {
  id: ChatModel
  nameKey: TranslationKey
}

type EffortOption = {
  id: ReasoningEffort
  labelKey: TranslationKey
}

const MODEL_OPTIONS: readonly ModelOption[] = [
  { id: 'deepseek-flash', nameKey: 'modelFlashName' },
  { id: 'deepseek-v4-pro', nameKey: 'modelV4ProName' },
]

const EFFORT_OPTIONS: readonly EffortOption[] = [
  { id: 'off', labelKey: 'reasoningEffortOff' },
  { id: 'low', labelKey: 'reasoningEffortLow' },
  { id: 'high', labelKey: 'reasoningEffortHigh' },
  { id: 'max', labelKey: 'reasoningEffortMax' },
]

/** 输入框右下角模型与推理等级两级卡片（1:1 对齐 DSH ModelSelect） */
export const ModelPicker = ({ disabled }: { disabled?: boolean }) => {
  const t = useTranslate()
  const [open, setOpen] = useState(false)
  const [pane, setPane] = useState<Pane>('root')
  const model = useSettings((state) => state.model)
  const reasoningEffort = useSettings((state) => state.reasoningEffort)

  const currentModel = MODEL_OPTIONS.find((item) => item.id === model) ?? MODEL_OPTIONS[0]
  const currentEffort =
    EFFORT_OPTIONS.find((item) => item.id === reasoningEffort) ?? EFFORT_OPTIONS[0]

  const closeMenu = () => {
    setOpen(false)
    setPane('root')
  }

  const { anchorRef, floatingRef, position } = useAnchoredPosition<HTMLSpanElement, HTMLDivElement>(
    open,
    { side: 'top', align: 'end', offset: 8, measureKey: pane }
  )
  useDismiss(open, [anchorRef, floatingRef], () => {
    if (pane !== 'root') {
      setPane('root')
      return
    }
    closeMenu()
  })

  // 在二级面板按 Escape 先返回一级面板，在一级面板按 Escape 关闭浮层
  useEffect(() => {
    if (!open) {
      return
    }
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node
      if (anchorRef.current?.contains(target) || floatingRef.current?.contains(target)) {
        return
      }
      closeMenu()
    }
    document.addEventListener('pointerdown', onPointerDown, true)
    return () => document.removeEventListener('pointerdown', onPointerDown, true)
  }, [anchorRef, floatingRef, open])

  return (
    <>
      <span ref={anchorRef} className={styles.anchor}>
        <button
          type="button"
          className={clsx(styles.trigger, styles.modelTrigger, open && styles.triggerOpen)}
          aria-label={t('modelMenuLabel')}
          aria-haspopup="menu"
          aria-expanded={open}
          disabled={disabled}
          onClick={() => {
            if (open) {
              closeMenu()
            } else {
              setPane('root')
              setOpen(true)
            }
          }}
        >
          <span className={styles.triggerLabel}>{t(currentModel.nameKey)}</span>
          <span className={styles.triggerEffort}>{t(currentEffort.labelKey)}</span>
          <span className={clsx(styles.chevron, open && styles.chevronOpen)}>
            <IconChevronDownOutline14 />
          </span>
        </button>
      </span>
      {open
        ? createPortal(
            <div
              ref={floatingRef}
              className={clsx(styles.popover, styles.dshModelCard)}
              style={position ?? undefined}
              role="menu"
              aria-label={t('modelMenuLabel')}
            >
              {pane === 'root' ? (
                <>
                  <button
                    type="button"
                    role="menuitem"
                    className={styles.drillCell}
                    onClick={() => setPane('model')}
                  >
                    <span className={styles.drillCellLabel}>{t('modelSectionTitle')}</span>
                    <span className={styles.drillCellValue}>{t(currentModel.nameKey)}</span>
                    <span className={styles.drillCellChevron}>
                      <IconChevronRightOutline14 />
                    </span>
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    className={styles.drillCell}
                    onClick={() => setPane('effort')}
                  >
                    <span className={styles.drillCellLabel}>{t('reasoningEffortLabel')}</span>
                    <span className={styles.drillCellValue}>{t(currentEffort.labelKey)}</span>
                    <span className={styles.drillCellChevron}>
                      <IconChevronRightOutline14 />
                    </span>
                  </button>
                </>
              ) : null}

              {pane === 'model'
                ? MODEL_OPTIONS.map((option) => {
                    const selected = option.id === model
                    return (
                      <button
                        key={option.id}
                        type="button"
                        role="menuitemradio"
                        aria-checked={selected}
                        className={styles.dshOption}
                        onClick={() => {
                          setModel(option.id)
                          closeMenu()
                        }}
                      >
                        <span className={styles.dshOptionLabel}>{t(option.nameKey)}</span>
                        <span className={styles.dshCheck}>
                          {selected ? <IconCheckedOutline14 /> : null}
                        </span>
                      </button>
                    )
                  })
                : null}

              {pane === 'effort'
                ? EFFORT_OPTIONS.map((option) => {
                    const selected = option.id === reasoningEffort
                    return (
                      <button
                        key={option.id}
                        type="button"
                        role="menuitemradio"
                        aria-checked={selected}
                        className={styles.dshOption}
                        onClick={() => {
                          setReasoningEffort(option.id)
                          closeMenu()
                        }}
                      >
                        <span className={styles.dshOptionLabel}>{t(option.labelKey)}</span>
                        <span className={styles.dshCheck}>
                          {selected ? <IconCheckedOutline14 /> : null}
                        </span>
                      </button>
                    )
                  })
                : null}
            </div>,
            document.body
          )
        : null}
    </>
  )
}
