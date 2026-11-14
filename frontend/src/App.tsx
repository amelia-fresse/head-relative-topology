import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { connect, createWailsBackend, errorMessage, hasRepo, type Backend } from './backend'
import { BranchMap, type MapActions, type MapTarget } from './components/BranchMap'
import { Banners, EmptyMap, ErrorState, HiddenToggle, Loading, LocalOnlyNote, NoRepo, type Notice } from './components/Chrome'
import { ContextMenu, type MenuItem, type MenuState } from './components/ContextMenu'
import { Header } from './components/Header'
import { TabBar } from './components/TabBar'
import { CopyIcon, EyeIcon, EyeOffIcon, ExternalIcon } from './components/icons'
import { Tooltip, type TipState } from './components/Tooltip'
import { useLatest, useNow, useShortcuts } from './hooks'
import { computeLayout } from './layout'
import { defaultSettings, nextTheme, themeOf, type Settings, type Snapshot, type Tab } from './model'

type Phase = { kind: 'loading' } | { kind: 'error'; message: string } | { kind: 'ready' }

const TIP_DELAY_MS = 450

export default function App() {
  const [backend, setBackend] = useState<Backend | null>(null)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [tabs, setTabs] = useState<Tab[]>([])
  const [settings, setSettings] = useState<Settings>(defaultSettings)
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' })
  const [notices, setNotices] = useState<Notice[]>([])
  const [dismissedWarnings, setDismissedWarnings] = useState<ReadonlySet<string>>(new Set())
  const [refreshing, setRefreshing] = useState(false)
  const [picking, setPicking] = useState(false)
  const [tip, setTip] = useState<TipState | null>(null)
  const [menu, setMenu] = useState<MenuState | null>(null)
  const now = useNow()

  const settingsRef = useLatest(settings)
  const backendRef = useLatest(backend)
  const menuOpenRef = useLatest(menu !== null)
  // Path of the tab being drawn; pushed snapshots of other repos are ignored.
  const activePathRef = useRef<string | null>(null)
  const tipTimer = useRef<number | undefined>(undefined)

  const notify = useCallback((tone: Notice['tone'], text: string) => {
    setNotices((ns) => [...ns.filter((n) => n.text !== text), { id: `n:${Date.now()}:${Math.random()}`, tone, text }])
  }, [])

  // --- loading ---------------------------------------------------------------
  const load = useCallback(async () => {
    setPhase({ kind: 'loading' })
    try {
      const c = await connect()
      const st = await c.backend.getSettings().catch(() => defaultSettings())
      setSettings({ ...defaultSettings(), ...st })
      setBackend(c.backend)
      activePathRef.current = c.snapshot.repo?.path || null
      setSnapshot(c.snapshot)
      c.backend.tabs().then(setTabs, () => {})
      if (c.fallbackReason) notify('info', c.fallbackReason)
      setPhase({ kind: 'ready' })
    } catch (e) {
      // Keep a backend around so "Open another repository" and pushed snapshots still work.
      const b = createWailsBackend()
      setBackend(b)
      b.getSettings().then(
        (st) => setSettings({ ...defaultSettings(), ...st }),
        () => {},
      )
      setPhase({ kind: 'error', message: errorMessage(e) })
    }
  }, [notify])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!backend) return
    const offSnap = backend.onSnapshot((s) => {
      const active = activePathRef.current
      if (active && s.repo?.path && s.repo.path !== active) return
      setSnapshot(s)
      setPhase({ kind: 'ready' })
    })
    const offTabs = backend.onTabs((t) => {
      setTabs(t)
      activePathRef.current = t.find((x) => x.active)?.path ?? null
    })
    return () => {
      offSnap()
      offTabs()
    }
  }, [backend])

  /** Shows a snapshot returned by a tab action and makes its repo the drawn one. */
  const showSnapshot = useCallback((s: Snapshot) => {
    activePathRef.current = s.repo?.path || null
    setSnapshot(s)
    setPhase({ kind: 'ready' })
  }, [])

  // --- settings --------------------------------------------------------------
  const theme = themeOf(settings)
  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])

  const updateSettings = useCallback(
    (patch: Partial<Settings>) => {
      const next = { ...settingsRef.current, ...patch }
      setSettings(next)
      backendRef.current?.saveSettings(next).catch((e) => notify('error', `Couldn’t save settings: ${errorMessage(e)}`))
    },
    [settingsRef, backendRef, notify],
  )

  // --- backend actions -------------------------------------------------------
  const refresh = useCallback(() => {
    const b = backendRef.current
    if (!b || refreshing) return
    setRefreshing(true)
    b.refresh(true)
      .then((s) => {
        if (!activePathRef.current || s.repo?.path === activePathRef.current) showSnapshot(s)
      })
      .catch((e) => notify('error', `Refresh failed: ${errorMessage(e)}`))
      .finally(() => setRefreshing(false))
  }, [backendRef, refreshing, notify, showSnapshot])

  const hideTip = useCallback(() => {
    window.clearTimeout(tipTimer.current)
    setTip(null)
  }, [])

  const pickRepo = useCallback(() => {
    const b = backendRef.current
    if (!b) return
    setPicking(true)
    b.pickRepo()
      .then((s) => {
        if (hasRepo(s)) showSnapshot(s)
        return b.tabs().then(setTabs)
      })
      .catch((e) => notify('error', `Couldn’t open that folder: ${errorMessage(e)}`))
      .finally(() => setPicking(false))
  }, [backendRef, notify, showSnapshot])

  const switchTab = useCallback(
    (path: string) => {
      const b = backendRef.current
      if (!b || path === activePathRef.current) return
      activePathRef.current = path
      setTabs((ts) => ts.map((t) => ({ ...t, active: t.path === path })))
      hideTip()
      b.switchRepo(path)
        .then(showSnapshot)
        .catch((e) => notify('error', `Couldn’t switch repository: ${errorMessage(e)}`))
    },
    [backendRef, notify, showSnapshot, hideTip],
  )

  const closeTab = useCallback(
    (path: string) => {
      const b = backendRef.current
      if (!b) return
      hideTip()
      b.closeRepo(path)
        .then((s) => {
          showSnapshot(s)
          return b.tabs().then(setTabs)
        })
        .catch((e) => notify('error', `Couldn’t close the tab: ${errorMessage(e)}`))
    },
    [backendRef, notify, showSnapshot, hideTip],
  )

  const tabsRef = useLatest(tabs)
  const stepTab = useCallback(
    (delta: number) => {
      const ts = tabsRef.current
      const i = ts.findIndex((t) => t.active)
      if (ts.length < 2 || i < 0) return
      switchTab(ts[(i + delta + ts.length) % ts.length].path)
    },
    [tabsRef, switchTab],
  )

  const openURL = useCallback(
    (url: string) => {
      backendRef.current?.openURL(url).catch((e) => notify('error', `Couldn’t open the link: ${errorMessage(e)}`))
    },
    [backendRef, notify],
  )

  const setHidden = useCallback(
    (name: string, hide: boolean) => {
      const b = backendRef.current
      if (!b) return
      ;(hide ? b.hideBranch(name) : b.unhideBranch(name))
        .then((s) => {
          setSnapshot(s)
          const still = s.branches.find((x) => x.name === name)
          if (!hide && still?.hidden) notify('info', `${name} is still hidden by a rule: ${still.hiddenReason || 'hide rule'}.`)
        })
        .catch((e) => notify('error', `Couldn’t ${hide ? 'hide' : 'unhide'} ${name}: ${errorMessage(e)}`))
    },
    [backendRef, notify],
  )

  const copy = useCallback(
    (text: string) => {
      backendRef.current?.copyText(text).catch((e) => notify('error', `Couldn’t copy: ${errorMessage(e)}`))
    },
    [backendRef, notify],
  )

  // --- map interactions ------------------------------------------------------

  const actions = useMemo<MapActions>(() => {
    const menuItems = (t: MapTarget): MenuItem[] => {
      if (t.kind === 'merged') {
        return [
          {
            label: 'Open pull request',
            icon: <ExternalIcon size={14} />,
            onSelect: () => openURL(t.pr.url),
            disabled: !t.pr.url,
          },
          {
            label: 'Copy branch name',
            icon: <CopyIcon size={14} />,
            onSelect: () => copy(t.pr.headRef),
            disabled: !t.pr.headRef,
          },
        ]
      }
      const b = t.branch
      return [
        {
          label: 'Open pull request',
          icon: <ExternalIcon size={14} />,
          onSelect: () => b.pr && openURL(b.pr.url),
          disabled: !b.pr,
        },
        {
          label: 'Copy branch name',
          icon: <CopyIcon size={14} />,
          onSelect: () => copy(b.name),
        },
        b.hidden
          ? {
              label: 'Unhide branch',
              icon: <EyeIcon size={14} />,
              onSelect: () => setHidden(b.name, false),
            }
          : {
              label: 'Hide branch',
              icon: <EyeOffIcon size={14} />,
              onSelect: () => setHidden(b.name, true),
            },
      ]
    }
    const showMenu: MapActions['menu'] = (t, at, returnFocus) => {
      const label = t.kind === 'branch' ? `Actions for ${t.branch.name}` : `Actions for pull request #${t.pr.number}`
      setMenu({ x: at.x, y: at.y, label, items: menuItems(t), returnFocus })
    }
    return {
      activate(t, el) {
        if (t.kind === 'merged') return openURL(t.pr.url)
        if (t.branch.pr?.url) return openURL(t.branch.pr.url)
        // No PR to open: offer the actions instead.
        const r = el.getBoundingClientRect()
        showMenu(t, { x: r.left + 16, y: r.top + Math.min(r.height, 28) }, el)
      },
      menu: showMenu,
      tipShow(t, el) {
        window.clearTimeout(tipTimer.current)
        if (menuOpenRef.current) return
        tipTimer.current = window.setTimeout(() => {
          if (!el.isConnected || menuOpenRef.current) return
          setTip({ target: t, rect: el.getBoundingClientRect() })
        }, TIP_DELAY_MS)
      },
      tipHide: hideTip,
    }
  }, [openURL, copy, setHidden, hideTip, menuOpenRef])

  // Tooltips are positioned once; drop them when the list scrolls.
  useEffect(() => {
    if (!tip) return
    const onScroll = () => hideTip()
    window.addEventListener('scroll', onScroll, true)
    return () => window.removeEventListener('scroll', onScroll, true)
  }, [tip, hideTip])

  // --- derived ---------------------------------------------------------------
  const ready = phase.kind === 'ready' && hasRepo(snapshot)
  const layout = useMemo(
    () =>
      snapshot && ready
        ? computeLayout(snapshot, {
            showMerged: settings.showMerged,
            showHidden: settings.showHidden,
            onlyRemote: settings.onlyRemote,
          })
        : null,
    [snapshot, ready, settings.showMerged, settings.showHidden, settings.onlyRemote],
  )

  useShortcuts(
    {
      r: refresh,
      o: pickRepo,
      c: () => updateSettings({ compact: !settingsRef.current.compact }),
      m: () => updateSettings({ showMerged: !settingsRef.current.showMerged }),
      t: () => updateSettings({ theme: nextTheme(settingsRef.current.theme) }),
      h: () => updateSettings({ showHidden: !settingsRef.current.showHidden }),
      l: () => updateSettings({ onlyRemote: !settingsRef.current.onlyRemote }),
      '[': () => stepTab(-1),
      ']': () => stepTab(1),
      ...Object.fromEntries(
        Array.from({ length: 9 }, (_, i) => [String(i + 1), () => tabsRef.current[i] && switchTab(tabsRef.current[i].path)]),
      ),
      escape: hideTip,
    },
    menu === null,
  )

  const allNotices: Notice[] = [
    ...(snapshot?.warnings ?? []).filter((w) => !dismissedWarnings.has(w)).map((w): Notice => ({ id: `w:${w}`, tone: 'warning', text: w })),
    ...notices,
  ]
  const dismiss = (id: string) => {
    if (id.startsWith('w:')) setDismissedWarnings((s) => new Set(s).add(id.slice(2)))
    else setNotices((ns) => ns.filter((n) => n.id !== id))
  }

  return (
    <div className={`app density-${settings.compact ? 'compact' : 'normal'}`}>
      <div className="topbar">
        <Header
          snapshot={hasRepo(snapshot) ? snapshot : null}
          settings={{ ...settings, theme }}
          now={now}
          refreshing={refreshing}
          onRefresh={refresh}
          onToggleCompact={() => updateSettings({ compact: !settings.compact })}
          onToggleMerged={() => updateSettings({ showMerged: !settings.showMerged })}
          onToggleOnlyRemote={() => updateSettings({ onlyRemote: !settings.onlyRemote })}
          onCycleTheme={() => updateSettings({ theme: nextTheme(settings.theme) })}
        />
        {tabs.length > 0 && <TabBar tabs={tabs} onSelect={switchTab} onClose={closeTab} onAdd={pickRepo} />}
      </div>
      <Banners notices={allNotices} onDismiss={dismiss} />
      <main className="content">
        {phase.kind === 'loading' && <Loading />}
        {phase.kind === 'error' && <ErrorState message={phase.message} onRetry={() => void load()} onPick={pickRepo} />}
        {phase.kind === 'ready' && !hasRepo(snapshot) && <NoRepo onPick={pickRepo} busy={picking} />}
        {layout && snapshot && (
          <>
            <BranchMap rows={layout.rows} snapshot={snapshot} compact={settings.compact} now={now} actions={actions} />
            {layout.shownCount === 0 && <EmptyMap hiddenCount={layout.hiddenCount} localOnlyCount={layout.localOnlyCount} />}
            <LocalOnlyNote count={layout.localOnlyCount} onShow={() => updateSettings({ onlyRemote: false })} />
            <HiddenToggle
              count={layout.hiddenCount}
              shown={settings.showHidden}
              onToggle={() => updateSettings({ showHidden: !settings.showHidden })}
            />
          </>
        )}
      </main>
      {tip && snapshot && <Tooltip tip={tip} now={now} mainRef={snapshot.main.ref || 'main'} />}
      {menu && <ContextMenu menu={menu} onClose={() => setMenu(null)} />}
    </div>
  )
}
