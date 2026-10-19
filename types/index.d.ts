/** A session's state, as the pad shows it. */
export type State = 'idle' | 'working' | 'awaiting' | 'unread' | 'error'

/** The line shown at the end of the prompt hint, or null for none. */
export type Hint = string | null

/** What a session last reported, kept across reloads of the plugin. */
export type Reported = { state: State; turnId: string | null; effort: string | null }

declare module 'claude-code' {
  interface PluginState {
    'lets-build-clauder': { hint: Hint; reported: Reported | null }
  }
}
