/** 会话级权限档位，1:1 对齐 DSH 三档权限预设 */
export type PermissionMode = 'read-only' | 'workspace-write' | 'full-access'

/** 本地工作区实体 */
export type Workspace = {
  id: string
  /** 规范化绝对路径（唯一键语义） */
  path: string
  /** 默认取路径最后一段，可重命名 */
  name: string
  createdAt: number
  updatedAt: number
}
