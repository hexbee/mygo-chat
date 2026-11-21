import clsx from 'clsx'
import { useEffect, useState } from 'react'
import { useTranslate } from '../../i18n/index.js'
import {
  MCPService,
  type MCPImportResult,
  type MCPProbeResult,
  type MCPRuntimeCheck,
  type MCPServerConfig,
  type MCPServerStatus,
} from '../../mygo.js'
import { applyMCPSnapshot, refreshMCPSnapshot, useMCPStore } from '../../services/mcp/store.js'
import { Button } from '../base/Button.js'
import { Modal } from '../base/Modal.js'
import { showToast } from '../base/toastState.js'
import styles from './McpSetting.module.css'

type KVEntry = {
  id: string
  key: string
  value: string
  visible?: boolean
}

let kvSeq = 0
const nextKvId = () => `kv-${++kvSeq}`

const recordToKV = (record?: Record<string, string>): KVEntry[] =>
  Object.entries(record ?? {}).map(([key, value]) => ({
    id: nextKvId(),
    key,
    value,
    visible: false,
  }))

const kvToRecord = (entries: KVEntry[]): Record<string, string> | undefined => {
  const out: Record<string, string> = {}
  for (const item of entries) {
    const k = item.key.trim()
    if (k) {
      out[k] = item.value
    }
  }
  return Object.keys(out).length > 0 ? out : undefined
}

const parseArgsLine = (raw: string): string[] => {
  const out: string[] = []
  let current = ''
  let quote: '"' | "'" | null = null
  for (let i = 0; i < raw.length; i += 1) {
    const ch = raw[i]
    if (quote) {
      if (ch === quote) {
        quote = null
      } else {
        current += ch
      }
      continue
    }
    if (ch === '"' || ch === "'") {
      quote = ch
      continue
    }
    if (/\s/.test(ch)) {
      if (current) {
        out.push(current)
        current = ''
      }
      continue
    }
    current += ch
  }
  if (current) {
    out.push(current)
  }
  return out
}

const formatArgsLine = (args?: string[]): string =>
  (args ?? []).map((a) => (/\s/.test(a) ? `"${a}"` : a)).join(' ')

/** 默认遮罩 URL 中的查询参数/凭据，以及 Stdio 命令的参数，防止窥屏泄露隐私或 Key */
const maskEndpointSummary = (cfg: MCPServerConfig): string => {
  if (cfg.transport === 'stdio') {
    const cmd = cfg.command?.trim() ?? ''
    if (!cmd) {
      return ''
    }
    return cfg.args?.length ? `${cmd} ••••••` : cmd
  }
  const rawUrl = cfg.url?.trim() ?? ''
  if (!rawUrl) {
    return ''
  }
  try {
    const parsed = new URL(rawUrl)
    const authPrefix = parsed.username || parsed.password ? '•••@' : ''
    const base = `${parsed.protocol}//${authPrefix}${parsed.host}${parsed.pathname}`
    const keys = [...new Set([...parsed.searchParams.keys()])]
    if (keys.length === 0) {
      return base
    }
    const maskedQuery = keys.map((k) => `${k}=••••••`).join('&')
    return `${base}?${maskedQuery}`
  } catch {
    return '••••••••••••'
  }
}

const EXAMPLE_IMPORT_JSON = `{
  "mcpServers": {
    "example-http": {
      "transport": "streamable-http",
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer YOUR_TOKEN"
      }
    }
  }
}`

export const McpSetting = () => {
  const t = useTranslate()
  const servers = useMCPStore((state) => state.servers)
  const statuses = useMCPStore((state) => state.statuses)
  const [editing, setEditing] = useState<{ mode: 'add' | 'edit'; initial?: MCPServerConfig } | null>(
    null
  )
  const [importOpen, setImportOpen] = useState(false)
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({})
  const [probingServer, setProbingServer] = useState<Record<string, boolean>>({})
  const [probeResults, setProbeResults] = useState<Record<string, MCPProbeResult>>({})
  const [busyServer, setBusyServer] = useState<Record<string, boolean>>({})

  useEffect(() => {
    void refreshMCPSnapshot()
  }, [])

  const handleToggle = async (cfg: MCPServerConfig) => {
    setBusyServer((prev) => ({ ...prev, [cfg.name]: true }))
    try {
      const snap = await MCPService.setServerEnabled(cfg.name, !cfg.enabled)
      applyMCPSnapshot(snap)
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error))
    } finally {
      setBusyServer((prev) => ({ ...prev, [cfg.name]: false }))
    }
  }

  const handleRestart = async (name: string) => {
    setBusyServer((prev) => ({ ...prev, [name]: true }))
    try {
      const snap = await MCPService.restartServer(name)
      applyMCPSnapshot(snap)
      showToast(t('settingsMcpRestartedToast', { name }))
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error))
    } finally {
      setBusyServer((prev) => ({ ...prev, [name]: false }))
    }
  }

  const handleDelete = async (name: string) => {
    setBusyServer((prev) => ({ ...prev, [name]: true }))
    try {
      const snap = await MCPService.deleteServer(name)
      applyMCPSnapshot(snap)
      setProbeResults((prev) =>
        Object.fromEntries(Object.entries(prev).filter(([key]) => key !== name))
      )
      showToast(t('settingsMcpDeletedToast', { name }))
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error))
    } finally {
      setBusyServer((prev) => ({ ...prev, [name]: false }))
    }
  }

  const handleProbe = async (cfg: MCPServerConfig) => {
    setProbingServer((prev) => ({ ...prev, [cfg.name]: true }))
    try {
      const result = await MCPService.probeServer(cfg)
      setProbeResults((prev) => ({ ...prev, [cfg.name]: result }))
    } catch (error) {
      setProbeResults((prev) => ({
        ...prev,
        [cfg.name]: {
          ok: false,
          stage: 'connect',
          toolCount: 0,
          durationMs: 0,
          message: error instanceof Error ? error.message : String(error),
        },
      }))
    } finally {
      setProbingServer((prev) => ({ ...prev, [cfg.name]: false }))
    }
  }

  return (
    <div className={styles.root}>
      <div className={styles.headerRow}>
        <div className={styles.headerText}>
          <div className={styles.title}>{t('settingsMcpTitle')}</div>
          <div className={styles.desc}>{t('settingsMcpDescription')}</div>
        </div>
        <div className={styles.headerActions}>
          <Button
            variant="outlined"
            size="m"
            className={styles.actionBtn}
            onClick={() => setImportOpen(true)}
          >
            {t('settingsMcpImportJson')}
          </Button>
          <Button
            variant="primary"
            size="m"
            className={styles.actionBtn}
            onClick={() => setEditing({ mode: 'add' })}
          >
            {t('settingsMcpAddServer')}
          </Button>
        </div>
      </div>

      {servers.length === 0 ? (
        <div className={styles.empty}>{t('settingsMcpEmpty')}</div>
      ) : (
        <div className={styles.serverList}>
          {servers.map((cfg) => {
            const status = statuses[cfg.name]
            const busy = Boolean(busyServer[cfg.name])
            const probing = Boolean(probingServer[cfg.name])
            const probeRes = probeResults[cfg.name]
            const tools = status?.tools ?? []
            const showTools = Boolean(expandedTools[cfg.name])

            return (
              <ServerCard
                key={cfg.name}
                cfg={cfg}
                status={status}
                busy={busy}
                probing={probing}
                probeRes={probeRes}
                toolsOpen={showTools}
                toolCount={tools.length}
                onToggle={() => void handleToggle(cfg)}
                onProbe={() => void handleProbe(cfg)}
                onToggleTools={() =>
                  setExpandedTools((prev) => ({ ...prev, [cfg.name]: !prev[cfg.name] }))
                }
                onRestart={() => void handleRestart(cfg.name)}
                onEdit={() => setEditing({ mode: 'edit', initial: cfg })}
                onDelete={() => void handleDelete(cfg.name)}
              />
            )
          })}
        </div>
      )}

      {editing ? (
        <ServerFormModal
          mode={editing.mode}
          initial={editing.initial}
          onClose={() => setEditing(null)}
          onSaved={() => setEditing(null)}
        />
      ) : null}

      {importOpen ? (
        <ImportJsonModal onClose={() => setImportOpen(false)} />
      ) : null}
    </div>
  )
}

const ServerCard = ({
  cfg,
  status,
  busy,
  probing,
  probeRes,
  toolsOpen,
  toolCount,
  onToggle,
  onProbe,
  onToggleTools,
  onRestart,
  onEdit,
  onDelete,
}: {
  cfg: MCPServerConfig
  status?: MCPServerStatus
  busy: boolean
  probing: boolean
  probeRes?: MCPProbeResult
  toolsOpen: boolean
  toolCount: number
  onToggle: () => void
  onProbe: () => void
  onToggleTools: () => void
  onRestart: () => void
  onEdit: () => void
  onDelete: () => void
}) => {
  const t = useTranslate()
  const [endpointVisible, setEndpointVisible] = useState(false)
  const state = status?.state ?? (cfg.enabled ? 'connecting' : 'disabled')

  const statusMeta = (() => {
    switch (state) {
      case 'connected':
        return {
          label: t('settingsMcpStateConnected', { count: toolCount }),
          className: styles.statusConnected,
        }
      case 'connecting':
        return {
          label: t('settingsMcpStateConnecting'),
          className: styles.statusConnecting,
        }
      case 'reconnecting':
        return {
          label: t('settingsMcpStateReconnecting', {
            attempt: status?.attempt ?? 1,
            max: status?.maxAttempts ?? 10,
          }),
          className: styles.statusWarn,
        }
      case 'missing-runtime':
        return {
          label: t('settingsMcpStateMissingRuntime'),
          className: styles.statusWarn,
        }
      case 'error':
        return {
          label: t('settingsMcpStateError'),
          className: styles.statusError,
        }
      default:
        return {
          label: t('settingsMcpStateDisabled'),
          className: styles.statusDisabled,
        }
    }
  })()

  const fullEndpoint =
    cfg.transport === 'stdio'
      ? [cfg.command ?? '', ...(cfg.args ?? [])].filter(Boolean).join(' ')
      : (cfg.url ?? '')
  const maskedEndpoint = maskEndpointSummary(cfg)
  const hasHiddenPart = fullEndpoint !== maskedEndpoint
  const displayedEndpoint = endpointVisible ? fullEndpoint : maskedEndpoint

  return (
    <div className={clsx(styles.serverCard, !cfg.enabled && styles.serverCardMuted)}>
      <div className={styles.cardHeader}>
        <div className={styles.cardTitleGroup}>
          <span className={styles.serverName}>{cfg.name}</span>
          <span className={styles.badge}>
            {cfg.transport === 'stdio' ? 'Stdio' : 'HTTP'}
          </span>
          <span className={styles.badge}>
            {cfg.toolLoading === 'eager' ? 'Eager' : 'On-demand'}
          </span>
          {cfg.trustServer ? (
            <span className={styles.badge}>{t('settingsMcpTrustedBadge')}</span>
          ) : null}
          <span className={clsx(styles.statusBadge, statusMeta.className)}>
            {statusMeta.label}
          </span>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={cfg.enabled}
          aria-label={`${cfg.name} ${cfg.enabled ? 'on' : 'off'}`}
          disabled={busy}
          className={clsx(styles.switch, cfg.enabled && styles.switchOn)}
          onClick={onToggle}
        >
          <span className={styles.switchThumb} />
        </button>
      </div>

      <div className={styles.cardBody}>
        {displayedEndpoint ? (
          <div className={styles.endpointRow}>
            <div
              className={styles.endpointText}
              title={endpointVisible ? fullEndpoint : undefined}
            >
              {displayedEndpoint}
            </div>
            {hasHiddenPart ? (
              <button
                type="button"
                className={styles.smallBtn}
                onClick={() => setEndpointVisible((v) => !v)}
              >
                {endpointVisible ? t('settingsMcpHideSecret') : t('settingsMcpShowSecret')}
              </button>
            ) : null}
          </div>
        ) : null}

        {status?.lastError ? (
          <div
            className={clsx(
              styles.alertBox,
              state === 'missing-runtime' ? styles.alertWarn : styles.alertError
            )}
          >
            {status.lastError}
          </div>
        ) : null}

        {probeRes ? (
          <div
            className={clsx(
              styles.alertBox,
              probeRes.ok
                ? styles.alertSuccess
                : probeRes.stage === 'runtime'
                  ? styles.alertWarn
                  : styles.alertError
            )}
          >
            {probeRes.ok
              ? t('settingsMcpProbeSuccess', {
                  count: probeRes.toolCount,
                  ms: probeRes.durationMs,
                })
              : t('settingsMcpProbeFailed', {
                  stage: probeRes.stage,
                  message: probeRes.message ?? '',
                })}
          </div>
        ) : null}

        <div className={styles.cardActions}>
          <button
            type="button"
            className={styles.smallBtn}
            disabled={probing}
            onClick={onProbe}
          >
            {probing ? t('settingsMcpProbing') : t('settingsMcpProbe')}
          </button>
          {toolCount > 0 ? (
            <button type="button" className={styles.smallBtn} onClick={onToggleTools}>
              {toolsOpen
                ? t('settingsMcpHideTools')
                : t('settingsMcpViewTools', { count: toolCount })}
            </button>
          ) : null}
          {cfg.enabled ? (
            <button
              type="button"
              className={styles.smallBtn}
              disabled={busy}
              onClick={onRestart}
            >
              {t('settingsMcpRestart')}
            </button>
          ) : null}
          <button type="button" className={styles.smallBtn} onClick={onEdit}>
            {t('settingsMcpEdit')}
          </button>
          <button
            type="button"
            className={clsx(styles.smallBtn, styles.smallBtnDanger)}
            disabled={busy}
            onClick={onDelete}
          >
            {t('delete')}
          </button>
        </div>

        {toolsOpen && status?.tools?.length ? (
          <div className={styles.toolList}>
            {status.tools.map((tool) => (
              <div key={tool.publicName} className={styles.toolRow}>
                <div className={styles.toolRowHead}>
                  <span className={styles.toolName}>{tool.rawName}</span>
                  {tool.readOnlyHint ? (
                    <span className={styles.badge}>{t('settingsMcpReadOnlyBadge')}</span>
                  ) : null}
                </div>
                {tool.description ? (
                  <div className={styles.toolDesc}>{tool.description}</div>
                ) : null}
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  )
}

const ServerFormModal = ({
  mode,
  initial,
  onClose,
  onSaved,
}: {
  mode: 'add' | 'edit'
  initial?: MCPServerConfig
  onClose: () => void
  onSaved: () => void
}) => {
  const t = useTranslate()
  const [name, setName] = useState(initial?.name ?? '')
  const [transport, setTransport] = useState<'streamable-http' | 'stdio'>(
    initial?.transport === 'stdio' ? 'stdio' : 'streamable-http'
  )
  const [url, setUrl] = useState(initial?.url ?? '')
  const [headers, setHeaders] = useState<KVEntry[]>(() => recordToKV(initial?.headers))
  const [command, setCommand] = useState(initial?.command ?? '')
  const [argsLine, setArgsLine] = useState(() => formatArgsLine(initial?.args))
  const [cwd, setCwd] = useState(initial?.cwd ?? '')
  const [envEntries, setEnvEntries] = useState<KVEntry[]>(() => recordToKV(initial?.env))
  const [enabled, setEnabled] = useState(initial?.enabled ?? true)
  const [toolLoading, setToolLoading] = useState<'on-demand' | 'eager'>(
    initial?.toolLoading === 'eager' ? 'eager' : 'on-demand'
  )
  const [timeoutMs, setTimeoutMs] = useState(String(initial?.toolCallTimeoutMs ?? 60000))
  const [reconnectEnabled, setReconnectEnabled] = useState(initial?.reconnect?.enabled ?? true)
  const [trustServer, setTrustServer] = useState(initial?.trustServer ?? false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [urlVisible, setUrlVisible] = useState(mode === 'add')
  const [argsVisible, setArgsVisible] = useState(mode === 'add')

  const [runtimeCheck, setRuntimeCheck] = useState<MCPRuntimeCheck | null>(null)
  const [probing, setProbing] = useState(false)
  const [probeRes, setProbeRes] = useState<MCPProbeResult | null>(null)
  const [saving, setSaving] = useState(false)
  const [errorText, setErrorText] = useState('')

  // Debounced real-time runtime check when editing stdio command
  useEffect(() => {
    if (transport !== 'stdio') {
      setRuntimeCheck(null)
      return
    }
    const trimmed = command.trim()
    if (!trimmed) {
      setRuntimeCheck(null)
      return
    }
    let cancelled = false
    const timer = window.setTimeout(() => {
      MCPService.checkRuntime(trimmed)
        .then((res) => {
          if (!cancelled) {
            setRuntimeCheck(res)
          }
        })
        .catch(() => {})
    }, 300)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [transport, command])

  const buildConfig = (): MCPServerConfig => {
    const parsedTimeout = Number.parseInt(timeoutMs, 10)
    return {
      name: name.trim(),
      transport,
      enabled,
      toolLoading,
      trustServer,
      toolCallTimeoutMs: Number.isFinite(parsedTimeout) ? parsedTimeout : 60000,
      reconnect: {
        enabled: reconnectEnabled,
        initialDelayMs: initial?.reconnect?.initialDelayMs ?? 500,
        maxDelayMs: initial?.reconnect?.maxDelayMs ?? 30000,
        maxAttempts: initial?.reconnect?.maxAttempts ?? 10,
      },
      ...(transport === 'streamable-http'
        ? {
            url: url.trim(),
            headers: kvToRecord(headers),
          }
        : {
            command: command.trim(),
            args: parseArgsLine(argsLine),
            cwd: cwd.trim() || undefined,
            env: kvToRecord(envEntries),
          }),
    }
  }

  const handleProbe = async () => {
    setProbing(true)
    setProbeRes(null)
    setErrorText('')
    try {
      const res = await MCPService.probeServer(buildConfig())
      setProbeRes(res)
    } catch (error) {
      setProbeRes({
        ok: false,
        stage: 'connect',
        toolCount: 0,
        durationMs: 0,
        message: error instanceof Error ? error.message : String(error),
      })
    } finally {
      setProbing(false)
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setErrorText('')
    try {
      const cfg = buildConfig()
      const snap = await MCPService.upsertServer(cfg)
      applyMCPSnapshot(snap)
      showToast(t('settingsMcpSavedToast', { name: cfg.name }))
      onSaved()
    } catch (error) {
      setErrorText(error instanceof Error ? error.message : String(error))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title={mode === 'edit' ? t('settingsMcpEditTitle') : t('settingsMcpAddTitle')}
      closable
      className={styles.modal}
      onClose={onClose}
      footer={
        <div className={styles.modalFooter}>
          <Button
            variant="outlined"
            size="m"
            disabled={probing || saving}
            onClick={() => void handleProbe()}
          >
            {probing ? t('settingsMcpProbing') : t('settingsMcpProbe')}
          </Button>
          <div className={styles.footerRight}>
            <Button variant="outlined" size="m" disabled={saving} onClick={onClose}>
              {t('cancel')}
            </Button>
            <Button
              variant="primary"
              size="m"
              disabled={saving}
              onClick={() => void handleSave()}
            >
              {t('confirm')}
            </Button>
          </div>
        </div>
      }
    >
      <div className={styles.form}>
        <div className={styles.field}>
          <label className={styles.fieldLabel}>{t('settingsMcpFieldName')}</label>
          <input
            type="text"
            className={styles.input}
            value={name}
            disabled={mode === 'edit'}
            placeholder="e.g. github, context7, local-fs"
            onChange={(e) => setName(e.target.value)}
          />
          <div className={styles.fieldHint}>{t('settingsMcpFieldNameHint')}</div>
        </div>

        <div className={styles.field}>
          <label className={styles.fieldLabel}>{t('settingsMcpFieldTransport')}</label>
          <div className={styles.segmented}>
            <button
              type="button"
              className={clsx(
                styles.segmentBtn,
                transport === 'streamable-http' && styles.segmentBtnActive
              )}
              onClick={() => setTransport('streamable-http')}
            >
              Streamable HTTP
            </button>
            <button
              type="button"
              className={clsx(
                styles.segmentBtn,
                transport === 'stdio' && styles.segmentBtnActive
              )}
              onClick={() => setTransport('stdio')}
            >
              Stdio
            </button>
          </div>
        </div>

        {transport === 'streamable-http' ? (
          <>
            <div className={styles.field}>
              <label className={styles.fieldLabel}>URL</label>
              <div className={styles.inputRow}>
                <input
                  type={urlVisible ? 'text' : 'password'}
                  className={styles.input}
                  value={url}
                  placeholder="https://mcp.example.com/mcp"
                  onChange={(e) => setUrl(e.target.value)}
                />
                <button
                  type="button"
                  className={styles.smallBtn}
                  onClick={() => setUrlVisible((v) => !v)}
                >
                  {urlVisible ? t('settingsMcpHideSecret') : t('settingsMcpShowSecret')}
                </button>
              </div>
            </div>

            <KVEditor
              label={t('settingsMcpFieldHeaders')}
              hint={t('settingsMcpFieldHeadersHint')}
              entries={headers}
              keyPlaceholder="Authorization"
              valuePlaceholder="Bearer ..."
              maskByDefault
              onChange={setHeaders}
            />
          </>
        ) : (
          <>
            <div className={styles.field}>
              <label className={styles.fieldLabel}>{t('settingsMcpFieldCommand')}</label>
              <input
                type="text"
                className={styles.input}
                value={command}
                placeholder="npx / uvx / /usr/local/bin/my-mcp"
                onChange={(e) => setCommand(e.target.value)}
              />
              {runtimeCheck && !runtimeCheck.found ? (
                <div className={clsx(styles.alertBox, styles.alertWarn)}>
                  {runtimeCheck.message}
                </div>
              ) : runtimeCheck?.found && runtimeCheck.resolvedPath ? (
                <div className={styles.fieldHint}>
                  {t('settingsMcpRuntimeFound', { path: runtimeCheck.resolvedPath })}
                </div>
              ) : null}
            </div>

            <div className={styles.field}>
              <label className={styles.fieldLabel}>{t('settingsMcpFieldArgs')}</label>
              <div className={styles.inputRow}>
                <input
                  type={argsVisible ? 'text' : 'password'}
                  className={styles.input}
                  value={argsLine}
                  placeholder="-y @modelcontextprotocol/server-filesystem /path"
                  onChange={(e) => setArgsLine(e.target.value)}
                />
                <button
                  type="button"
                  className={styles.smallBtn}
                  onClick={() => setArgsVisible((v) => !v)}
                >
                  {argsVisible ? t('settingsMcpHideSecret') : t('settingsMcpShowSecret')}
                </button>
              </div>
            </div>

            <div className={styles.field}>
              <label className={styles.fieldLabel}>{t('settingsMcpFieldCwd')}</label>
              <input
                type="text"
                className={styles.input}
                value={cwd}
                placeholder={t('settingsMcpOptionalPlaceholder')}
                onChange={(e) => setCwd(e.target.value)}
              />
            </div>

            <KVEditor
              label={t('settingsMcpFieldEnv')}
              entries={envEntries}
              keyPlaceholder="API_KEY"
              valuePlaceholder="..."
              maskByDefault
              onChange={setEnvEntries}
            />
          </>
        )}

        <div className={styles.fieldHint}>{t('settingsMcpSecurityNotice')}</div>

        <button
          type="button"
          className={styles.advancedToggle}
          onClick={() => setShowAdvanced((v) => !v)}
        >
          {showAdvanced ? '▾' : '▸'} {t('settingsMcpAdvanced')}
        </button>

        {showAdvanced ? (
          <div className={styles.advancedPanel}>
            <div className={styles.field}>
              <label className={styles.fieldLabel}>{t('settingsMcpToolLoading')}</label>
              <div className={styles.segmented}>
                <button
                  type="button"
                  className={clsx(
                    styles.segmentBtn,
                    toolLoading === 'on-demand' && styles.segmentBtnActive
                  )}
                  onClick={() => setToolLoading('on-demand')}
                >
                  {t('settingsMcpLoadingOnDemand')}
                </button>
                <button
                  type="button"
                  className={clsx(
                    styles.segmentBtn,
                    toolLoading === 'eager' && styles.segmentBtnActive
                  )}
                  onClick={() => setToolLoading('eager')}
                >
                  {t('settingsMcpLoadingEager')}
                </button>
              </div>
              <div className={styles.fieldHint}>{t('settingsMcpToolLoadingHint')}</div>
            </div>

            <div className={styles.field}>
              <label className={styles.fieldLabel}>{t('settingsMcpTimeoutMs')}</label>
              <input
                type="number"
                className={styles.input}
                value={timeoutMs}
                min={1000}
                max={300000}
                onChange={(e) => setTimeoutMs(e.target.value)}
              />
            </div>

            <div className={styles.toggleRow}>
              <div>
                <div className={styles.fieldLabel}>{t('settingsMcpAutoReconnect')}</div>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={reconnectEnabled}
                className={clsx(styles.switch, reconnectEnabled && styles.switchOn)}
                onClick={() => setReconnectEnabled((v) => !v)}
              >
                <span className={styles.switchThumb} />
              </button>
            </div>

            <div className={styles.toggleRow}>
              <div>
                <div className={styles.fieldLabel}>{t('settingsMcpTrustServer')}</div>
                <div className={styles.fieldHint}>{t('settingsMcpTrustServerHint')}</div>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={trustServer}
                className={clsx(styles.switch, trustServer && styles.switchOn)}
                onClick={() => setTrustServer((v) => !v)}
              >
                <span className={styles.switchThumb} />
              </button>
            </div>

            <div className={styles.toggleRow}>
              <div className={styles.fieldLabel}>{t('settingsMcpEnabledOnSave')}</div>
              <button
                type="button"
                role="switch"
                aria-checked={enabled}
                className={clsx(styles.switch, enabled && styles.switchOn)}
                onClick={() => setEnabled((v) => !v)}
              >
                <span className={styles.switchThumb} />
              </button>
            </div>
          </div>
        ) : null}

        {probeRes ? (
          <div
            className={clsx(
              styles.alertBox,
              probeRes.ok
                ? styles.alertSuccess
                : probeRes.stage === 'runtime'
                  ? styles.alertWarn
                  : styles.alertError
            )}
          >
            {probeRes.ok
              ? t('settingsMcpProbeSuccess', {
                  count: probeRes.toolCount,
                  ms: probeRes.durationMs,
                })
              : t('settingsMcpProbeFailed', {
                  stage: probeRes.stage,
                  message: probeRes.message ?? '',
                })}
          </div>
        ) : null}

        {errorText ? (
          <div className={clsx(styles.alertBox, styles.alertError)}>{errorText}</div>
        ) : null}
      </div>
    </Modal>
  )
}

const KVEditor = ({
  label,
  hint,
  entries,
  keyPlaceholder,
  valuePlaceholder,
  maskByDefault,
  onChange,
}: {
  label: string
  hint?: string
  entries: KVEntry[]
  keyPlaceholder: string
  valuePlaceholder: string
  maskByDefault?: boolean
  onChange: (next: KVEntry[]) => void
}) => {
  const t = useTranslate()
  return (
    <div className={styles.field}>
      <div className={styles.toggleRow}>
        <label className={styles.fieldLabel}>{label}</label>
        <button
          type="button"
          className={styles.smallBtn}
          onClick={() =>
            onChange([...entries, { id: nextKvId(), key: '', value: '', visible: !maskByDefault }])
          }
        >
          + {t('settingsBlacklistAdd')}
        </button>
      </div>
      {hint ? <div className={styles.fieldHint}>{hint}</div> : null}
      {entries.length > 0 ? (
        <div className={styles.kvList}>
          {entries.map((entry) => (
            <div key={entry.id} className={styles.kvRow}>
              <input
                type="text"
                className={clsx(styles.input, styles.kvKey)}
                value={entry.key}
                placeholder={keyPlaceholder}
                onChange={(e) =>
                  onChange(
                    entries.map((item) =>
                      item.id === entry.id ? { ...item, key: e.target.value } : item
                    )
                  )
                }
              />
              <input
                type={entry.visible ? 'text' : 'password'}
                className={clsx(styles.input, styles.kvVal)}
                value={entry.value}
                placeholder={valuePlaceholder}
                onChange={(e) =>
                  onChange(
                    entries.map((item) =>
                      item.id === entry.id ? { ...item, value: e.target.value } : item
                    )
                  )
                }
              />
              <button
                type="button"
                className={styles.smallBtn}
                onClick={() =>
                  onChange(
                    entries.map((item) =>
                      item.id === entry.id ? { ...item, visible: !item.visible } : item
                    )
                  )
                }
              >
                {entry.visible ? t('settingsMcpHideSecret') : t('settingsMcpShowSecret')}
              </button>
              <button
                type="button"
                className={clsx(styles.smallBtn, styles.smallBtnDanger)}
                onClick={() => onChange(entries.filter((item) => item.id !== entry.id))}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

const ImportJsonModal = ({ onClose }: { onClose: () => void }) => {
  const t = useTranslate()
  const [rawJson, setRawJson] = useState(EXAMPLE_IMPORT_JSON)
  const [validation, setValidation] = useState<MCPImportResult | null>(null)
  const [busy, setBusy] = useState(false)

  const handleLoadCurrent = async () => {
    try {
      const exported = await MCPService.exportConfigJSON()
      setRawJson(exported)
      setValidation(null)
    } catch {
      // ignore
    }
  }

  const handleValidate = async () => {
    setBusy(true)
    try {
      const res = await MCPService.importConfigJSON(rawJson, false)
      setValidation(res)
    } finally {
      setBusy(false)
    }
  }

  const handleApply = async () => {
    setBusy(true)
    try {
      const res = await MCPService.importConfigJSON(rawJson, true)
      setValidation(res)
      if (res.valid && res.applied) {
        await refreshMCPSnapshot()
        showToast(t('settingsMcpImportedToast', { count: res.count }))
        onClose()
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={t('settingsMcpImportTitle')}
      closable
      className={styles.modal}
      onClose={onClose}
      footer={
        <div className={styles.modalFooter}>
          <Button
            variant="outlined"
            size="m"
            disabled={busy}
            onClick={() => void handleLoadCurrent()}
          >
            {t('settingsMcpLoadCurrentJson')}
          </Button>
          <div className={styles.footerRight}>
            <Button
              variant="outlined"
              size="m"
              disabled={busy}
              onClick={() => void handleValidate()}
            >
              {t('settingsMcpValidateJson')}
            </Button>
            <Button
              variant="primary"
              size="m"
              disabled={busy}
              onClick={() => void handleApply()}
            >
              {t('settingsMcpApplyImport')}
            </Button>
          </div>
        </div>
      }
    >
      <div className={styles.form}>
        <div className={styles.fieldHint}>{t('settingsMcpImportHint')}</div>
        <textarea
          className={styles.textarea}
          value={rawJson}
          spellCheck={false}
          onChange={(e) => {
            setRawJson(e.target.value)
            setValidation(null)
          }}
        />
        {validation && !validation.valid && validation.errors?.length ? (
          <div className={clsx(styles.alertBox, styles.alertError)}>
            <ul className={styles.errorList}>
              {validation.errors.map((err, idx) => (
                <li key={`${err.field}-${idx}`}>
                  <strong>{err.field}</strong>: {err.message}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        {validation?.valid ? (
          <div className={clsx(styles.alertBox, styles.alertSuccess)}>
            {t('settingsMcpValidateOk', { count: validation.count })}
          </div>
        ) : null}
      </div>
    </Modal>
  )
}
