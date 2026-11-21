import { create } from 'zustand'
import { WorkspaceService } from '../../mygo.js'
import { createId } from '../../utils/id.js'
import * as db from '../chat/db.js'
import { setDefaultPermissionMode, useSettings } from '../settings.js'
import type { PermissionMode, Workspace } from './types.js'

const COLLAPSED_KEY = 'chat-demo:workspace-collapsed'

const readCollapsed = (): Record<string, boolean> => {
  try {
    const raw = localStorage.getItem(COLLAPSED_KEY)
    if (!raw) {
      return {}
    }
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {}
    }
    const result: Record<string, boolean> = {}
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === 'boolean') {
        result[key] = value
      }
    }
    return result
  } catch {
    return {}
  }
}

const writeCollapsed = (collapsed: Record<string, boolean>) => {
  try {
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(collapsed))
  } catch {
    // 存储不可用时仅在内存中生效
  }
}

/** 规范化路径（去除尾部多余分隔符，保留根路径如 / 或 C:\） */
export const normalizeWorkspacePath = (raw: string): string => {
  const trimmed = raw.trim()
  if (!trimmed) {
    return ''
  }
  if (/^[A-Za-z]:[\\/]*$/.test(trimmed)) {
    return `${trimmed.slice(0, 2)}\\`
  }
  if (/^\/+$/.test(trimmed)) {
    return '/'
  }
  return trimmed.replace(/[\\/]+$/, '')
}

/** 取路径最后一段作为默认工作区显示名 */
export const basenameOfPath = (path: string): string => {
  const normalized = normalizeWorkspacePath(path)
  if (!normalized || normalized === '/') {
    return normalized || 'Workspace'
  }
  const parts = normalized.split(/[\\/]/).filter(Boolean)
  return parts.at(-1) ?? normalized
}

/** 当多个工作区同名时，计算最短可区分的父目录后缀 */
export const distinguishingPathSuffixes = (
  items: readonly Pick<Workspace, 'id' | 'name' | 'path'>[]
): Map<string, string> => {
  const suffixes = new Map<string, string>()
  const groups = new Map<string, Pick<Workspace, 'id' | 'name' | 'path'>[]>()
  for (const item of items) {
    const list = groups.get(item.name) ?? []
    list.push(item)
    groups.set(item.name, list)
  }
  for (const group of groups.values()) {
    if (group.length < 2) {
      continue
    }
    const segments = group.map((item) => ({
      id: item.id,
      parts: normalizeWorkspacePath(item.path).split(/[\\/]/).filter(Boolean),
    }))
    for (let depth = 2; depth <= 6; depth += 1) {
      const candidates = segments.map((entry) => ({
        id: entry.id,
        suffix: entry.parts.slice(-depth, -1).join('/'),
      }))
      const unique = new Set(candidates.map((c) => c.suffix))
      if (unique.size === candidates.length || depth === 6) {
        for (const candidate of candidates) {
          if (candidate.suffix) {
            suffixes.set(candidate.id, candidate.suffix)
          }
        }
        break
      }
    }
  }
  return suffixes
}

type WorkspaceState = {
  ready: boolean
  workspaces: Workspace[]
  collapsedIds: Record<string, boolean>
  /** 新对话（activeId === null）当前选中的工作区 */
  draftWorkspaceId: string | null
  /** 新对话（activeId === null）当前选中的权限档位 */
  draftPermissionMode: PermissionMode
}

export const useWorkspaceStore = create<WorkspaceState>(() => ({
  ready: false,
  workspaces: [],
  collapsedIds: readCollapsed(),
  draftWorkspaceId: null,
  draftPermissionMode: useSettings.getState().defaultPermissionMode,
}))

const { getState: get, setState: set } = useWorkspaceStore

const sortWorkspaces = (list: Workspace[]) => [...list].sort((a, b) => b.updatedAt - a.updatedAt)

let initTask: Promise<void> | undefined

export const initWorkspaces = (): Promise<void> => {
  if (get().ready) {
    return Promise.resolve()
  }
  if (initTask) {
    return initTask
  }
  initTask = (async () => {
    try {
      const workspaces = await db.loadWorkspaces()
      set({ ready: true, workspaces: sortWorkspaces(workspaces) })
    } catch (error) {
      db.reportStorageError('read workspaces')(error)
      set({ ready: true })
    } finally {
      initTask = undefined
    }
  })()
  return initTask
}

export const addWorkspace = async (
  rawPath: string,
  customName?: string
): Promise<Workspace | null> => {
  const path = normalizeWorkspacePath(rawPath)
  if (!path) {
    return null
  }
  const now = Date.now()
  const existing = get().workspaces.find((w) => w.path === path)
  if (existing) {
    const updated: Workspace = {
      ...existing,
      ...(customName?.trim() ? { name: customName.trim() } : {}),
      updatedAt: now,
    }
    set((state) => ({
      workspaces: sortWorkspaces(state.workspaces.map((w) => (w.id === existing.id ? updated : w))),
    }))
    await db.saveWorkspace(updated).catch(db.reportStorageError('update workspace'))
    return updated
  }
  const workspace: Workspace = {
    id: createId(),
    path,
    name: customName?.trim() || basenameOfPath(path),
    createdAt: now,
    updatedAt: now,
  }
  set((state) => ({
    workspaces: sortWorkspaces([workspace, ...state.workspaces]),
  }))
  await db.saveWorkspace(workspace).catch(db.reportStorageError('save workspace'))
  return workspace
}

/** 调起原生目录选择器并添加为工作区；用户取消返回 null */
export const pickAndAddWorkspace = async (): Promise<Workspace | null> => {
  const selected = await WorkspaceService.pickDirectory()
  if (!selected) {
    return null
  }
  return addWorkspace(selected)
}

export const renameWorkspace = async (id: string, name: string): Promise<void> => {
  const trimmed = name.trim()
  if (!trimmed) {
    return
  }
  const target = get().workspaces.find((w) => w.id === id)
  if (!target || target.name === trimmed) {
    return
  }
  const updated: Workspace = { ...target, name: trimmed, updatedAt: Date.now() }
  set((state) => ({
    workspaces: sortWorkspaces(state.workspaces.map((w) => (w.id === id ? updated : w))),
  }))
  await db.saveWorkspace(updated).catch(db.reportStorageError('rename workspace'))
}

export const deleteWorkspaceRecord = async (id: string): Promise<void> => {
  set((state) => {
    const nextCollapsed = Object.fromEntries(
      Object.entries(state.collapsedIds).filter(([key]) => key !== id)
    )
    writeCollapsed(nextCollapsed)
    return {
      workspaces: state.workspaces.filter((w) => w.id !== id),
      collapsedIds: nextCollapsed,
      draftWorkspaceId: state.draftWorkspaceId === id ? null : state.draftWorkspaceId,
    }
  })
  await Promise.all([
    db.deleteWorkspace(id).catch(db.reportStorageError('delete workspace')),
    db.unbindWorkspaceSessions(id).catch(db.reportStorageError('unbind workspace sessions')),
  ])
}

export const toggleWorkspaceCollapsed = (id: string) => {
  set((state) => {
    const next = { ...state.collapsedIds, [id]: !state.collapsedIds[id] }
    writeCollapsed(next)
    return { collapsedIds: next }
  })
}

export const setDraftWorkspaceId = (workspaceId: string | null) => {
  set({ draftWorkspaceId: workspaceId })
}

export const setDraftPermissionMode = (mode: PermissionMode) => {
  setDefaultPermissionMode(mode)
  set({ draftPermissionMode: mode })
}

export const openWorkspacePath = async (absPath: string): Promise<void> => {
  await WorkspaceService.openPath(absPath)
}

export const revealWorkspaceInFinder = async (absPath: string): Promise<void> => {
  await WorkspaceService.revealInFinder(absPath)
}

export const saveWorkspacePathAs = async (absPath: string): Promise<string> => {
  return WorkspaceService.savePathAs(absPath)
}

export const readWorkspaceFilePreview = async (absPath: string) => {
  return WorkspaceService.readFilePreview(absPath)
}
