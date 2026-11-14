import { Clipboard, Events } from '@wailsio/runtime'
import { Service } from '../../bindings/github.com/amelia-fresse/head-relative-topology/internal/app'
import { Snapshot as SnapshotEvent, Tabs as TabsEvent } from '../../bindings/events'
import type { Settings, Snapshot, Tab } from '../model'
import type { Backend } from './types'

export function createWailsBackend(): Backend {
  return {
    kind: 'wails',
    snapshot: () => Service.Snapshot(),
    refresh: (fetch) => Service.Refresh(fetch),
    pickRepo: () => Service.PickRepo(),
    tabs: async () => (await Service.Tabs()) ?? [],
    switchRepo: (path) => Service.SwitchRepo(path),
    closeRepo: (path) => Service.CloseRepo(path),
    getSettings: () => Service.GetSettings(),
    saveSettings: (s: Settings) => Service.SaveSettings(s),
    hideBranch: (name) => Service.HideBranch(name),
    unhideBranch: (name) => Service.UnhideBranch(name),
    openURL: (url) => Service.OpenURL(url),
    copyText: async (text) => {
      try {
        await Clipboard.SetText(text)
      } catch {
        await navigator.clipboard.writeText(text)
      }
    },
    onSnapshot: (cb: (s: Snapshot) => void) => Events.On(SnapshotEvent, (ev) => cb(ev.data)),
    onTabs: (cb: (t: Tab[]) => void) => Events.On(TabsEvent, (ev) => cb(ev.data ?? [])),
  }
}
