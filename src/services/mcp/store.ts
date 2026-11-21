import { create } from 'zustand'
import {
  events,
  MCPService,
  type MCPServerConfig,
  type MCPServerStatus,
  type MCPSnapshot,
  type MCPToolInfo,
} from '../../mygo.js'
import type { ClientToolDefinition } from '../api/types.js'

const CATALOG_DESC_MAX_LEN = 160
const LOADER_RAW_NAME = 'load_tools'

type MCPState = {
  servers: MCPServerConfig[]
  statuses: Record<string, MCPServerStatus>
  loaded: boolean
}

export const useMCPStore = create<MCPState>(() => ({
  servers: [],
  statuses: {},
  loaded: false,
}))

const { getState: get, setState: set } = useMCPStore

/**
 * Pure in-memory, session-isolated map of loaded on-demand MCP tools:
 *   sessionId -> serverName -> Set<rawToolName>
 * Never persisted to IndexedDB/localStorage; new sessions & restarts begin empty.
 */
const sessionLoadedTools = new Map<string, Map<string, Set<string>>>()

const mcpLoaderPublicName = (serverName: string): string => `mcp__${serverName}__${LOADER_RAW_NAME}`

const summarizeCatalogDescription = (description: string): string => {
  const firstLine = description.trim().split('\n', 1)[0] ?? ''
  const collapsed = firstLine.replace(/\s+/g, ' ').trim()
  if (collapsed.length <= CATALOG_DESC_MAX_LEN) {
    return collapsed
  }
  return `${collapsed.slice(0, CATALOG_DESC_MAX_LEN - 3)}...`
}

/**
 * When a server reconnects or updates its tool list, intersect every session's
 * loaded on-demand tool set with the newly offered tools (keep still-offered, drop removed).
 */
const reconcileLoadedTools = (statuses: Record<string, MCPServerStatus>) => {
  for (const [sessionId, byServer] of sessionLoadedTools) {
    for (const [serverName, rawNames] of byServer) {
      const status = statuses[serverName]
      if (!status || !status.enabled) {
        byServer.delete(serverName)
        continue
      }
      if (status.state !== 'connected') {
        continue
      }
      const offered = new Set((status.tools ?? []).map((t) => t.rawName))
      for (const rawName of rawNames) {
        if (!offered.has(rawName)) {
          rawNames.delete(rawName)
        }
      }
      if (rawNames.size === 0) {
        byServer.delete(serverName)
      }
    }
    if (byServer.size === 0) {
      sessionLoadedTools.delete(sessionId)
    }
  }
}

export const applyMCPSnapshot = (snapshot: MCPSnapshot | null | undefined) => {
  if (!snapshot) {
    return
  }
  const servers = Array.isArray(snapshot.servers) ? snapshot.servers : []
  const statuses = snapshot.statuses ?? {}
  reconcileLoadedTools(statuses)
  set({ servers, statuses, loaded: true })
}

let initialized = false

export const initMCP = async (): Promise<void> => {
  if (!initialized) {
    initialized = true
    if (typeof window !== 'undefined' && window.mygo) {
      events.mcpState.on((snapshot) => {
        applyMCPSnapshot(snapshot)
      })
    }
  }
  if (typeof window === 'undefined' || !window.mygo) {
    set({ loaded: true })
    return
  }
  try {
    const snapshot = await MCPService.getSnapshot()
    applyMCPSnapshot(snapshot)
  } catch {
    set({ loaded: true })
  }
}

export const refreshMCPSnapshot = async (): Promise<void> => {
  if (typeof window === 'undefined' || !window.mygo) {
    return
  }
  const snapshot = await MCPService.getSnapshot()
  applyMCPSnapshot(snapshot)
}

export const markSessionMCPToolsLoaded = (
  sessionId: string,
  serverName: string,
  rawNames: readonly string[]
) => {
  if (!sessionId || !serverName || rawNames.length === 0) {
    return
  }
  let byServer = sessionLoadedTools.get(sessionId)
  if (!byServer) {
    byServer = new Map<string, Set<string>>()
    sessionLoadedTools.set(sessionId, byServer)
  }
  let loaded = byServer.get(serverName)
  if (!loaded) {
    loaded = new Set<string>()
    byServer.set(serverName, loaded)
  }
  for (const name of rawNames) {
    if (name) {
      loaded.add(name)
    }
  }
}

export const clearSessionMCPTools = (sessionId: string) => {
  sessionLoadedTools.delete(sessionId)
}

const toClientToolDefinition = (tool: MCPToolInfo): ClientToolDefinition => ({
  name: tool.publicName,
  description: tool.description || `MCP tool "${tool.rawName}" from server "${tool.serverName}".`,
  input_schema:
    tool.inputSchema && typeof tool.inputSchema === 'object'
      ? tool.inputSchema
      : { type: 'object', properties: {} },
})

const createLoaderToolDefinition = (serverName: string): ClientToolDefinition => ({
  name: mcpLoaderPublicName(serverName),
  description: `Add tools from MCP server ${serverName} to your tool list. The system prompt lists them under "On-demand tools from MCP server ${serverName}".`,
  input_schema: {
    type: 'object',
    properties: {
      tools: {
        type: 'array',
        items: { type: 'string' },
        description: 'Tool names exactly as that list shows them.',
      },
    },
    required: ['tools'],
  },
})

/**
 * Returns the active MCP ClientToolDefinitions for a specific session:
 * - eager servers: all offered tools
 * - on-demand servers: the server's `load_tools` meta-tool + tools loaded in this session
 */
export const getActiveMCPToolsForSession = (sessionId: string): ClientToolDefinition[] => {
  const { servers, statuses } = get()
  const result: ClientToolDefinition[] = []
  const sessionMap = sessionLoadedTools.get(sessionId)

  for (const cfg of servers) {
    if (!cfg.enabled) {
      continue
    }
    const status = statuses[cfg.name]
    if (!status || status.state !== 'connected') {
      continue
    }
    const tools = status.tools ?? []
    if (tools.length === 0) {
      continue
    }

    if (cfg.toolLoading === 'eager') {
      for (const tool of tools) {
        result.push(toClientToolDefinition(tool))
      }
      continue
    }

    // on-demand (default)
    result.push(createLoaderToolDefinition(cfg.name))
    const loadedSet = sessionMap?.get(cfg.name)
    if (loadedSet && loadedSet.size > 0) {
      for (const tool of tools) {
        if (loadedSet.has(tool.rawName)) {
          result.push(toClientToolDefinition(tool))
        }
      }
    }
  }

  return result
}

/**
 * Builds the literal MCP server instructions + on-demand tool catalog sections
 * for system prompt injection (deterministic server sort for KV-cache stability).
 */
export const buildMCPSystemPromptSection = (): string => {
  const { servers, statuses } = get()
  const sections: string[] = []

  for (const cfg of servers) {
    if (!cfg.enabled) {
      continue
    }
    const status = statuses[cfg.name]
    if (!status || status.state !== 'connected') {
      continue
    }

    const instructions = status.instructions?.trim()
    if (instructions) {
      sections.push(`### MCP server: ${cfg.name}\n\n${instructions}`)
    }

    const tools = status.tools ?? []
    if (cfg.toolLoading !== 'eager' && tools.length > 0) {
      const loaderName = mcpLoaderPublicName(cfg.name)
      const lines = tools.map((tool) => {
        const summary = summarizeCatalogDescription(tool.description)
        return summary ? `- ${tool.rawName}: ${summary}` : `- ${tool.rawName}`
      })
      sections.push(
        [
          `### On-demand tools from MCP server ${cfg.name}`,
          `These tools are not in your tool list. Before calling one, load it with ${loaderName}.`,
          lines.join('\n'),
        ].join('\n\n')
      )
    }
  }

  return sections.join('\n\n')
}

/**
 * Returns true if the given public MCP tool name is safe for bounded concurrent execution
 * (either an on-demand `load_tools` meta-tool or an MCP tool declaring `readOnlyHint: true`).
 */
export const isMCPReadOnlyTool = (publicName: string): boolean => {
  if (!publicName.startsWith('mcp__')) {
    return false
  }
  const { servers, statuses } = get()
  for (const cfg of servers) {
    if (!cfg.enabled) {
      continue
    }
    if (mcpLoaderPublicName(cfg.name) === publicName) {
      return true
    }
    const status = statuses[cfg.name]
    if (status?.state === 'connected') {
      const match = status.tools?.find((t) => t.publicName === publicName)
      if (match) {
        return match.readOnlyHint
      }
    }
  }
  return false
}
