import { create } from 'zustand'
import { UpdateService, type UpdateState } from '../../mygo.js'

interface UpdateStoreState {
  state: UpdateState
  dialogOpen: boolean
  busy: boolean
  checkUpdate: (silent?: boolean) => Promise<UpdateState>
  startDownload: () => Promise<void>
  relaunch: () => Promise<void>
  openDialog: () => void
  closeDialog: () => void
}

let pollTimer: number | null = null

const startPolling = (set: (fn: (s: UpdateStoreState) => Partial<UpdateStoreState>) => void) => {
  if (pollTimer !== null) {
    return
  }
  pollTimer = window.setInterval(async () => {
    try {
      const current = await UpdateService.getUpdateStatus()
      set(() => ({ state: current }))
      if (current.phase !== 'downloading' && current.phase !== 'verifying') {
        if (pollTimer !== null) {
          clearInterval(pollTimer)
          pollTimer = null
        }
      }
    } catch {
      // ignore transient poll error
    }
  }, 500)
}

export const useUpdateStore = create<UpdateStoreState>((set) => ({
  state: {
    phase: 'idle',
    version: '',
    percent: 0,
    notes: '',
    error: '',
  },
  dialogOpen: false,
  busy: false,

  openDialog: () => set({ dialogOpen: true }),
  closeDialog: () => set({ dialogOpen: false }),

  checkUpdate: async () => {
    set({ busy: true })
    try {
      const res = await UpdateService.checkUpdate()
      set({ state: res, busy: false })
      return res
    } catch (err: unknown) {
      const errorMsg = err instanceof Error ? err.message : String(err)
      set((s) => ({
        busy: false,
        state: { ...s.state, phase: 'error', error: errorMsg },
      }))
      throw err
    }
  },

  startDownload: async () => {
    set({ busy: true })
    try {
      const res = await UpdateService.startDownload()
      set({ state: res, busy: false })
      startPolling(set)
    } catch (err: unknown) {
      const errorMsg = err instanceof Error ? err.message : String(err)
      set((s) => ({
        busy: false,
        state: { ...s.state, phase: 'error', error: errorMsg },
      }))
    }
  },

  relaunch: async () => {
    try {
      await UpdateService.relaunch()
    } catch {
      // 重启失败时保持当前状态
    }
  },
}))
